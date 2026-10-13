package ingest

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
)

func GoModulePath(repoPath string) string {
	data, err := os.ReadFile(filepath.Join(repoPath, "go.mod"))
	if err != nil {
		return ""
	}
	for raw := range strings.SplitSeq(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if module, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(module)
		}
	}
	return ""
}

type denoManifest struct {
	Name string `json:"name"`

	Workspace []string `json:"workspace"`

	Imports map[string]string `json:"imports"`
}

// DenoImportMap is the bare-specifier to repository-relative path map a deno
// workspace resolves imports through: the root deno.json import map, plus each
// workspace member's declared package name pointing at its directory.
func DenoImportMap(repoPath string) map[string]string {
	root, ok := readDenoManifest(filepath.Join(repoPath, "deno.json"))
	if !ok {
		root, ok = readDenoManifest(filepath.Join(repoPath, "deno.jsonc"))
	}
	if !ok {
		return nil
	}
	imports := map[string]string{}
	for specifier, target := range root.Imports {
		if target == "" {
			continue
		}
		imports[specifier] = target
	}
	for _, member := range root.Workspace {
		directory := path.Clean(member)
		if directory == "." || directory == "" {
			continue
		}
		manifest, ok := readDenoManifest(filepath.Join(repoPath, filepath.FromSlash(directory), "deno.json"))
		if !ok {
			manifest, ok = readDenoManifest(filepath.Join(repoPath, filepath.FromSlash(directory), "deno.jsonc"))
		}
		if !ok || manifest.Name == "" {
			continue
		}
		if _, exists := imports[manifest.Name]; !exists {
			imports[manifest.Name] = "./" + directory
		}
	}
	if len(imports) == 0 {
		return nil
	}
	return imports
}

// DetectPartition is the partition mode a checkout defaults to: one context per
// workspace member for a deno workspace, one per directory otherwise.
func DetectPartition(repoPath string) string {
	manifest, ok := readDenoManifest(filepath.Join(repoPath, "deno.json"))
	if !ok {
		manifest, ok = readDenoManifest(filepath.Join(repoPath, "deno.jsonc"))
	}
	if ok && len(manifest.Workspace) > 0 {
		return spec.PartitionPackage
	}
	return spec.PartitionDirectory
}

func readDenoManifest(file string) (denoManifest, bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return denoManifest{}, false
	}
	manifest := denoManifest{}
	if err := json.Unmarshal(stripJSONComments(data), &manifest); err != nil {
		return denoManifest{}, false
	}
	return manifest, true
}

// stripJSONComments removes the // and /* */ comments a deno.jsonc may carry,
// so the same parser reads both manifest spellings.
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for index := 0; index < len(data); index++ {
		if index+1 < len(data) && data[index] == '/' && data[index+1] == '/' {
			for index < len(data) && data[index] != '\n' {
				index++
			}
			if index < len(data) {
				out = append(out, data[index])
			}
			continue
		}
		if index+1 < len(data) && data[index] == '/' && data[index+1] == '*' {
			index += 2
			for index+1 < len(data) && !(data[index] == '*' && data[index+1] == '/') {
				index++
			}
			index++
			continue
		}
		out = append(out, data[index])
	}
	return out
}

// DefaultExcludes is the populate scope an ingest applies when
// Repository.spec.populate.exclude is empty: the sample trees, the vendored
// and dependency trees, the generated trees and the bundled scripts a
// repository does not own. A repository that names its own exclude list
// replaces this list, it does not extend it.
var DefaultExcludes = []string{
	"**/fixtures/**",
	"**/testdata/**",
	"**/node_modules/**",
	"**/vendor/**",
	"**/.git/**",
	"**/generated/**",
	"**/*.min.js",
}

// SourceExtensions is the extension set that makes a file source code. Every
// other file is a tree file: YAML, JSON, Markdown, Rego, lockfiles, images and
// .env data are observed by a context, they never make one.
var SourceExtensions = []string{
	".go", ".ts", ".tsx", ".js", ".mjs", ".jsx", ".rs", ".py",
	".rb", ".sh", ".c", ".cc", ".h", ".cpp",
}

// IsSourceFile reports whether a repository-relative path holds source code.
func IsSourceFile(file string) bool {
	return slices.Contains(SourceExtensions, strings.ToLower(path.Ext(file)))
}

// SourceOnlyFacts drops the data files from the indexed facts, so the
// partitioner makes a context from source code alone, and returns the dropped
// paths for the caller to observe as tree files of the context that owns
// their directory. A tree of data files alone is not a context.
func SourceOnlyFacts(facts specsync.Facts) (specsync.Facts, []string) {
	files := make([]specsync.SourceFile, 0, len(facts.Files))
	data := []string{}
	for _, file := range facts.Files {
		if IsSourceFile(file.Path) {
			files = append(files, file)
			continue
		}
		data = append(data, file.Path)
	}
	symbols := make([]specsync.Symbol, 0, len(facts.Symbols))
	for _, symbol := range facts.Symbols {
		if IsSourceFile(symbol.File) {
			symbols = append(symbols, symbol)
		}
	}
	facts.Files = files
	facts.Symbols = symbols
	return facts, data
}

// SourceOnlyPartitions drops a partition that holds tree files alone, so a
// directory of data never becomes a SystemContext. The root context a
// repository asks for is the one exception: it is a context of the tree files
// at the repository root by design.
func SourceOnlyPartitions(partitions []specsync.Partition, rootContext bool) []specsync.Partition {
	out := make([]specsync.Partition, 0, len(partitions))
	for _, partition := range partitions {
		if len(partition.Files) > 0 {
			out = append(out, partition)
			continue
		}
		if rootContext && partition.Directory == "." && partition.Name != "" {
			out = append(out, partition)
		}
	}
	return out
}

var generatedMarkers = []string{"THIS FILE WAS GENERATED", "DO NOT EDIT", "@generated"}

// GeneratedDirectories lists the tree directories that hold only generated
// code, so the partitioner folds them into their package instead of making one
// context per codegen directory.
func GeneratedDirectories(repoPath string, files []specsync.SourceFile) map[string]bool {
	byDirectory := map[string][]string{}
	for _, file := range files {
		directory := path.Dir(file.Path)
		if directory == "." || directory == "" {
			continue
		}
		byDirectory[directory] = append(byDirectory[directory], file.Path)
	}
	generated := map[string]bool{}
	for directory, paths := range byDirectory {
		all := true
		for _, source := range paths {
			if !generatedSource(filepath.Join(repoPath, filepath.FromSlash(source))) {
				all = false
				break
			}
		}
		if all {
			generated[directory] = true
		}
	}
	return generated
}

func generatedSource(file string) bool {
	handle, err := os.Open(file)
	if err != nil {
		return false
	}
	defer handle.Close()
	head := make([]byte, 512)
	read, _ := handle.Read(head)
	text := string(head[:read])
	for _, marker := range generatedMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func withoutGenerated(roots []string, generated map[string]bool) []string {
	if len(generated) == 0 {
		return roots
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if generated[root] {
			continue
		}
		out = append(out, root)
	}
	if len(out) == 0 {
		return []string{"."}
	}
	return out
}

var skippedDirectories = map[string]bool{
	".git":         true,
	".codegraph":   true,
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
}

func PackageRoots(repoPath string) ([]string, error) {
	roots := map[string]bool{".": true}
	err := filepath.WalkDir(repoPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != repoPath && skippedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		for _, manifest := range specsync.PackageManifests {
			if entry.Name() != manifest {
				continue
			}
			relative, err := filepath.Rel(repoPath, filepath.Dir(path))
			if err != nil {
				return err
			}
			roots[filepath.ToSlash(relative)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(roots))
	for root := range roots {
		out = append(out, root)
	}
	sort.Strings(out)
	return out, nil
}
