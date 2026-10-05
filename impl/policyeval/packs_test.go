package policyeval

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	orasoci "oras.land/oras-go/v2/content/oci"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	packsregistry "github.com/publicdomainrelay/graph-clm-kcp-spec/policies/packs"
)

const packSourceHeader = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: probe
spec:
  crd:
    spec:
      names:
        kind: Probe
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: ""
      libs: []
`

const packSourceRego = `package probe

violation[{"msg": "probe"}] {
	input.review.kind.kind == "ArchitectureModel"
}
`

const packSourceConstraint = `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: Probe
metadata:
  name: probe
spec:
  enforcementAction: warn
  match:
    kinds:
      - apiGroups: [specs.publicdomainrelay.dev]
        kinds: [ArchitectureModel]
`

func packTree(name, version string) map[string]string {
	return map[string]string{
		policy.PackManifestPath:              "name: " + name + "\nversion: " + version + "\n",
		"templates/probe/template.yaml":      packSourceHeader,
		"templates/probe/src.rego":           packSourceRego,
		"constraints/probe.yaml":             packSourceConstraint,
		"tests/probe/suite.yaml":             "apiVersion: test.gatekeeper.sh/v1alpha1\nkind: Suite\nmetadata:\n  name: probe\ntests: []\n",
		"tests/probe/inventory/allowed.yaml": "{}\n",
	}
}

func writePackTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

func TestResolveImportFromAGitSource(t *testing.T) {
	work := t.TempDir()
	origin := filepath.Join(work, "origin")
	packDir := filepath.Join(origin, filepath.FromSlash(policy.PackDir), "gitpack")
	writePackTree(t, packDir, packTree("gitpack", "v1"))
	gitRun(t, origin, "init", "-q", "-b", "main")
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "pack")
	bare := filepath.Join(work, "bare.git")
	gitRun(t, work, "clone", "--quiet", "--bare", origin, bare)
	gitRun(t, bare, "symbolic-ref", "HEAD", "refs/heads/main")

	source := "git:" + bare + "@main"
	resolved, err := ResolveImport(policy.PackImport{Pack: "gitpack", Version: "v1", Source: source}, ImportOptions{WorkDir: work})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Manifest.Name != "gitpack" || resolved.Manifest.Version != "v1" {
		t.Errorf("the manifest is %+v", resolved.Manifest)
	}
	if len(resolved.Library.Templates) != 1 || policy.TemplateSlug(resolved.Library.Templates[0]) != "probe" {
		t.Errorf("the templates are %+v", resolved.Library.Templates)
	}
	if resolved.Entry.Source != source || resolved.Entry.SHA256 == "" || resolved.Entry.Files == 0 {
		t.Errorf("the lock entry is %+v", resolved.Entry)
	}

	want, files, err := PackDigest(os.DirFS(packDir))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Entry.SHA256 != want || resolved.Entry.Files != files {
		t.Errorf("the resolved digest is %s (%d files), want %s (%d files)",
			resolved.Entry.SHA256, resolved.Entry.Files, want, files)
	}

	if _, err := ResolveImport(policy.PackImport{Pack: "gitpack", Version: "v1", Source: "git:" + bare + "@no-such-ref"}, ImportOptions{WorkDir: work}); err == nil {
		t.Error("a git ref that does not exist resolved")
	}
}

func TestResolveImportFromAnOCISource(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	packDir := filepath.Join(work, "pack")
	files := packTree("ocipack", "v1")
	writePackTree(t, packDir, files)

	store, err := orasoci.New(filepath.Join(work, "layout"))
	if err != nil {
		t.Fatal(err)
	}
	layers := []ocispec.Descriptor{}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := []byte(files[name])
		descriptor := content.NewDescriptorFromBytes("application/vnd.specd.policy.file.v1", body)
		descriptor.Annotations = map[string]string{imageTitleAnnotation: name}
		if err := store.Push(ctx, descriptor, bytes.NewReader(body)); err != nil {
			t.Fatal(err)
		}
		layers = append(layers, descriptor)
	}
	manifest, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1,
		"application/vnd.specd.policy.pack.v1", oras.PackManifestOptions{Layers: layers})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Tag(ctx, manifest, "v1"); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(ociRegistryHandler(store))
	defer server.Close()
	reference := strings.TrimPrefix(server.URL, "http://") + "/ocipack:v1"

	resolved, err := ResolveImport(policy.PackImport{Pack: "ocipack", Version: "v1", Source: "oci:" + reference}, ImportOptions{WorkDir: work})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Manifest.Name != "ocipack" {
		t.Errorf("the manifest is %+v", resolved.Manifest)
	}
	if len(resolved.Library.Templates) != 1 {
		t.Errorf("the templates are %+v", resolved.Library.Templates)
	}
	want, files_, err := PackDigest(os.DirFS(packDir))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Entry.SHA256 != want || resolved.Entry.Files != files_ {
		t.Errorf("the resolved digest is %s (%d files), want %s (%d files)",
			resolved.Entry.SHA256, resolved.Entry.Files, want, files_)
	}
}

func ociRegistryHandler(store *orasoci.Store) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		trimmed := strings.TrimPrefix(request.URL.Path, "/v2/")
		if trimmed == "" || trimmed == "/" {
			writer.WriteHeader(http.StatusOK)
			return
		}
		parts := strings.Split(trimmed, "/")
		if len(parts) < 3 {
			http.NotFound(writer, request)
			return
		}
		kind := parts[len(parts)-2]
		reference := parts[len(parts)-1]
		switch kind {
		case "manifests":
			descriptor, err := store.Resolve(ctx, reference)
			if err != nil {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", descriptor.MediaType)
			writer.Header().Set("Docker-Content-Digest", descriptor.Digest.String())
			writer.Header().Set("Content-Length", strconv.FormatInt(descriptor.Size, 10))
			if request.Method == http.MethodHead {
				writer.WriteHeader(http.StatusOK)
				return
			}
			body, err := content.FetchAll(ctx, store, descriptor)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return
			}
			writer.Write(body)
		case "blobs":
			descriptor, err := store.Resolve(ctx, reference)
			if err != nil {
				http.NotFound(writer, request)
				return
			}
			body, err := content.FetchAll(ctx, store, descriptor)
			if err != nil {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
			if request.Method == http.MethodHead {
				writer.WriteHeader(http.StatusOK)
				return
			}
			writer.Write(body)
		default:
			http.NotFound(writer, request)
		}
	})
}

func TestConformancePackResolvesAsAnEmbeddedImport(t *testing.T) {
	resolved, err := ResolveImport(policy.PackImport{Pack: "conformance", Version: "v1", Source: policy.SourceEmbedded}, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Manifest.Name != "conformance" {
		t.Errorf("the manifest is %+v", resolved.Manifest)
	}
	if len(resolved.Library.Templates) != 3 {
		t.Errorf("the conformance pack carries %d templates, want 3", len(resolved.Library.Templates))
	}
	if len(resolved.Library.Constraints) != 3 {
		t.Errorf("the conformance pack carries %d constraints, want 3", len(resolved.Library.Constraints))
	}
	if missing := resolved.Manifest.Missing(policy.Binding{}); len(missing) != 0 {
		t.Errorf("the conformance pack needs a binding: %v", missing)
	}
	library := policy.Library{Manifest: policy.PolicyLibrary{
		Repository: "fixture",
		Imports:    []policy.PackImport{{Pack: "conformance", Version: "v1", Source: policy.SourceEmbedded}},
	}}
	merged, entries, err := ResolveImports(library, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Templates) != 3 || len(merged.Constraints) != 3 {
		t.Errorf("the merged library carries %d template(s) and %d constraint(s)",
			len(merged.Templates), len(merged.Constraints))
	}
	if len(entries) != 1 || entries[0].Pack != "conformance" {
		t.Errorf("the lock entries are %+v", entries)
	}
	for _, template := range merged.Templates {
		if merged.ImportedFrom(policy.TemplateSlug(template)) == "" {
			t.Errorf("the template %s is not marked as imported", template.Name)
		}
	}
}

func TestEveryEmbeddedPackCarriesAPackManifest(t *testing.T) {
	names := packsregistry.Names()
	if len(names) == 0 {
		t.Fatal("no embedded pack")
	}
	for _, name := range names {
		fsys, err := fs.Sub(packsregistry.FS(), name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fs.Stat(fsys, policy.PackManifestPath); err != nil {
			t.Errorf("the embedded pack %s has no %s", name, policy.PackManifestPath)
		}
	}
}
