package policyeval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	orasoci "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	packsregistry "github.com/publicdomainrelay/graph-clm-kcp-spec/policies/packs"
)

const (
	packCloneDepth = "1"

	imageTitleAnnotation = "org.opencontainers.image.title"
)

// ImportOptions says where an imported pack comes from: the embedded registry
// by default, a scratch directory for a git clone or an OCI pull, and the lock
// a build verifies the resolution against.
type ImportOptions struct {
	Embedded fs.FS

	WorkDir string

	Lock *policy.PackLock

	Verify bool
}

// ImportedPack is one resolved import: what it was, what it resolved to, and
// the digest a lock pins.
type ImportedPack struct {
	Import policy.PackImport

	Manifest policy.PackManifest

	Library policy.Library

	Entry policy.LockEntry
}

// ResolveImports merges every pack a library imports into it. The templates
// and constraints of a pack are the pack's; the roles and the vocabulary stay
// the importing repository's.
func ResolveImports(library policy.Library, opts ImportOptions) (policy.Library, []policy.LockEntry, error) {
	entries := []policy.LockEntry{}
	for _, imp := range library.Manifest.Imports {
		resolved, err := ResolveImport(imp, opts)
		if err != nil {
			return policy.Library{}, nil, err
		}
		entries = append(entries, resolved.Entry)
		if err := mergePack(&library, resolved, imp); err != nil {
			return policy.Library{}, nil, err
		}
	}
	return library, entries, nil
}

// ResolveImport resolves one import to its pack: the embedded registry, a git
// ref, or an OCI artifact.
func ResolveImport(imp policy.PackImport, opts ImportOptions) (ImportedPack, error) {
	if strings.TrimSpace(imp.Pack) == "" {
		return ImportedPack{}, fmt.Errorf("policyeval: an import names no pack")
	}
	source, err := policy.ParsePackSource(imp.Source)
	if err != nil {
		return ImportedPack{}, err
	}
	fsys, cleanup, err := packSourceFS(imp, source, opts)
	if err != nil {
		return ImportedPack{}, err
	}
	defer cleanup()

	pack, err := LoadRaw(fsys)
	if err != nil {
		return ImportedPack{}, fmt.Errorf("policyeval: pack %s: %w", policy.ImportReference(imp), err)
	}
	if pack.Pack == nil {
		return ImportedPack{}, fmt.Errorf("policyeval: pack %s has no %s", policy.ImportReference(imp), policy.PackManifestPath)
	}
	if pack.Pack.Name != imp.Pack {
		return ImportedPack{}, fmt.Errorf("policyeval: pack %s declares the name %s", imp.Pack, pack.Pack.Name)
	}
	if imp.Version != "" && pack.Pack.Version != imp.Version {
		return ImportedPack{}, fmt.Errorf("policyeval: pack %s is version %s, the import pins %s",
			imp.Pack, pack.Pack.Version, imp.Version)
	}
	digest, files, err := PackDigest(fsys)
	if err != nil {
		return ImportedPack{}, err
	}
	entry := policy.LockEntry{
		Pack:    imp.Pack,
		Version: pack.Pack.Version,
		Source:  source.String(),
		SHA256:  digest,
		Files:   files,
	}
	if opts.Verify && opts.Lock != nil && !opts.Lock.Matches(entry) {
		pinned, ok := opts.Lock.Entry(entry.Pack, entry.Version)
		if !ok {
			return ImportedPack{}, fmt.Errorf("policyeval: pack %s is not in %s; run specctl policy build --relock",
				entry.Pack, policy.LockPath)
		}
		return ImportedPack{}, fmt.Errorf("policyeval: pack %s@%s resolves to %s, %s pins %s; bump the version or run specctl policy build --relock",
			entry.Pack, entry.Version, entry.SHA256, policy.LockPath, pinned.SHA256)
	}
	return ImportedPack{
		Import:   imp,
		Manifest: *pack.Pack,
		Library:  pack,
		Entry:    entry,
	}, nil
}

func mergePack(library *policy.Library, resolved ImportedPack, imp policy.PackImport) error {
	missing := resolved.Manifest.Missing(library.Manifest.Binding())
	if len(missing) > 0 {
		return fmt.Errorf("policyeval: pack %s needs %s, the binding of %s does not declare them",
			imp.Pack, strings.Join(missing, ", "), library.Manifest.Repository)
	}
	reference := policy.ImportReference(imp)
	if library.Imported == nil {
		library.Imported = map[string]string{}
	}
	for _, template := range resolved.Library.Templates {
		slug := policy.TemplateSlug(template)
		if existing, ok := library.Template(template.Name); ok {
			// A library that already carries the pack -- a branch seeded from a
			// resolved library, or a second resolution of the same tree -- is
			// not a collision when the copy is the pack's.
			if sameTemplate(existing, template) {
				library.Imported[slug] = reference
				continue
			}
			return fmt.Errorf("policyeval: pack %s has a template %s, %s already has %s",
				imp.Pack, template.Name, library.Manifest.Repository, existing.Name)
		}
		if existing, ok := library.TemplateOfKind(template.Kind); ok {
			return fmt.Errorf("policyeval: pack %s has a template of kind %s, %s already has %s",
				imp.Pack, template.Kind, library.Manifest.Repository, existing.Name)
		}
		library.Templates = append(library.Templates, template)
		library.Imported[slug] = reference
	}
	for _, constraint := range resolved.Library.Constraints {
		if existing, ok := library.Constraint(constraint.Name); ok {
			if sameConstraint(existing, constraint) {
				library.Imported[constraint.Name] = reference
				continue
			}
			return fmt.Errorf("policyeval: pack %s has a constraint %s, %s already has %s",
				imp.Pack, constraint.Name, library.Manifest.Repository, existing.Name)
		}
		library.Constraints = append(library.Constraints, constraint)
		library.Imported[constraint.Name] = reference
	}
	library.Sort()
	return nil
}

func sameTemplate(left, right policy.Template) bool {
	return left.Kind == right.Kind && left.Rego == right.Rego
}

func sameConstraint(left, right policy.Constraint) bool {
	if left.Kind != right.Kind || left.Enforcement != right.Enforcement {
		return false
	}
	leftDocument, leftErr := left.Document()
	rightDocument, rightErr := right.Document()
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftDocument) == string(rightDocument)
}

func packSourceFS(imp policy.PackImport, source policy.PackSource, opts ImportOptions) (fs.FS, func(), error) {
	switch source.Kind {
	case policy.SourceEmbedded:
		registry := opts.Embedded
		if registry == nil {
			registry = packsregistry.FS()
		}
		candidate := path.Join(imp.Pack)
		if _, err := fs.Stat(registry, path.Join(candidate, policy.PackManifestPath)); err != nil {
			return nil, func() {}, fmt.Errorf("policyeval: no embedded pack %s; embedded packs are %s",
				imp.Pack, strings.Join(packsregistry.Names(), ", "))
		}
		sub, err := fs.Sub(registry, candidate)
		if err != nil {
			return nil, func() {}, err
		}
		return sub, func() {}, nil
	case policy.SourceGit:
		return gitPackFS(imp, source, opts)
	case policy.SourceOCI:
		return ociPackFS(imp, source, opts)
	}
	return nil, func() {}, fmt.Errorf("policyeval: unknown pack source %q", source.String())
}

func gitPackFS(imp policy.PackImport, source policy.PackSource, opts ImportOptions) (fs.FS, func(), error) {
	dir, err := os.MkdirTemp(opts.WorkDir, "specd-pack-")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	shallow := exec.Command("git", "clone", "--quiet", "--depth", packCloneDepth, "--branch", source.Ref, source.URL, dir)
	if _, err := shallow.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			cleanup()
			return nil, func() {}, err
		}
		full := exec.Command("git", "clone", "--quiet", source.URL, dir)
		if output, err := full.CombinedOutput(); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("policyeval: git clone %s: %v: %s", source.URL, err, strings.TrimSpace(string(output)))
		}
		checkout := exec.Command("git", "-C", dir, "checkout", "--quiet", source.Ref)
		if output, err := checkout.CombinedOutput(); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("policyeval: git checkout %s: %v: %s", source.Ref, err, strings.TrimSpace(string(output)))
		}
	}
	root := dir
	nested := filepath.Join(dir, filepath.FromSlash(policy.PackDir), imp.Pack)
	if _, err := os.Stat(filepath.Join(nested, policy.PackManifestPath)); err == nil {
		root = nested
	}
	return os.DirFS(root), cleanup, nil
}

// ociPackFS pulls an OCI artifact and lays its layers out as a directory, so a
// pack published as an artifact is a pack directory like any other.
func ociPackFS(imp policy.PackImport, source policy.PackSource, opts ImportOptions) (fs.FS, func(), error) {
	reference := source.Ref
	if imp.Pack != "" && !strings.Contains(path.Base(reference), imp.Pack) && !strings.Contains(reference, "/") {
		reference = imp.Pack
	}
	repository, err := remote.NewRepository(reference)
	if err != nil {
		return nil, func() {}, fmt.Errorf("policyeval: oci reference %s: %w", reference, err)
	}
	tag := repository.Reference.Reference
	if tag == "" {
		tag = "latest"
	}
	dir, err := os.MkdirTemp(opts.WorkDir, "specd-pack-oci-")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	store, err := orasoci.New(dir)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	ctx := context.Background()
	if _, err := oras.Copy(ctx, repository, tag, store, tag, oras.DefaultCopyOptions); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("policyeval: oci pull %s: %w", reference, err)
	}
	out, err := os.MkdirTemp(opts.WorkDir, "specd-pack-oci-files-")
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	innerCleanup := func() {
		os.RemoveAll(out)
		cleanup()
	}
	if err := materializeOCI(ctx, store, tag, out); err != nil {
		innerCleanup()
		return nil, func() {}, err
	}
	return os.DirFS(out), innerCleanup, nil
}

func materializeOCI(ctx context.Context, store *orasoci.Store, tag, dir string) error {
	descriptor, err := store.Resolve(ctx, tag)
	if err != nil {
		return fmt.Errorf("policyeval: oci manifest %s: %w", tag, err)
	}
	successors, err := content.Successors(ctx, store, descriptor)
	if err != nil {
		return fmt.Errorf("policyeval: oci manifest %s: %w", tag, err)
	}
	wrote := 0
	for _, layer := range successors {
		name := layer.Annotations[imageTitleAnnotation]
		if name == "" || strings.Contains(name, "..") {
			continue
		}
		reader, err := store.Fetch(ctx, layer)
		if err != nil {
			return fmt.Errorf("policyeval: oci layer %s: %w", name, err)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			reader.Close()
			return err
		}
		file, err := os.Create(target)
		if err != nil {
			reader.Close()
			return err
		}
		_, copyErr := io.Copy(file, reader)
		file.Close()
		reader.Close()
		if copyErr != nil {
			return copyErr
		}
		wrote++
	}
	if wrote == 0 {
		return fmt.Errorf("policyeval: oci artifact %s carries no titled layer", tag)
	}
	return nil
}

// PackDigest is the sha256 of a pack's sources: pack.yaml, the templates, the
// constraints and the suites. The generated dist, the catalogue and the shared
// library are not part of it, so a build does not break a pin.
func PackDigest(fsys fs.FS) (string, int, error) {
	paths := []string{}
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !packSourcePath(name) {
			return nil
		}
		paths = append(paths, name)
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	sort.Strings(paths)
	digest := sha256.New()
	for _, name := range paths {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return "", 0, err
		}
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		digest.Write(data)
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil)), len(paths), nil
}

func packSourcePath(name string) bool {
	clean := path.Clean(name)
	if clean == policy.PackManifestPath || clean == policy.PoliciesPath {
		return true
	}
	for _, dir := range []string{policy.TemplatesDir, policy.ConstraintsDir, policy.TestsDir} {
		if strings.HasPrefix(clean, dir+"/") {
			return true
		}
	}
	return false
}

func ReadLock(dir string) (policy.PackLock, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, policy.LockPath))
	if err != nil {
		if os.IsNotExist(err) {
			return policy.PackLock{}, false, nil
		}
		return policy.PackLock{}, false, err
	}
	lock, err := ParseLock(data)
	if err != nil {
		return policy.PackLock{}, false, err
	}
	return lock, true, nil
}

func ParseLock(data []byte) (policy.PackLock, error) {
	var lock policy.PackLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return policy.PackLock{}, fmt.Errorf("policyeval: %s: %w", policy.LockPath, err)
	}
	return lock, nil
}

// EncodeLock writes a lock with its imports in a stable order.
func EncodeLock(entries []policy.LockEntry) ([]byte, error) {
	lock := policy.PackLock{Imports: []policy.LockEntry{}}
	for _, entry := range entries {
		lock.Set(entry)
	}
	return yaml.Marshal(lock)
}
