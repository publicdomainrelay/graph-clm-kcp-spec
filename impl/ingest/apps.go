package ingest

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
)

// TsEntry is one TypeScript entry point a repository deploys: the name it is
// known by and the repository-relative file that starts it.
type TsEntry struct {
	Name string

	Path string
}

type tsEntriesFile struct {
	Entries []tsEntryDecl `json:"entries"`
}

type tsEntryDecl struct {
	Name string `json:"name"`

	Path string `json:"path"`
}

// TsEntriesPath is the manifest a repository names its TypeScript entry points
// in. A repository without one deploys no TypeScript entry.
const TsEntriesPath = "deploy/ts-entries.yaml"

// TsEntries reads the TypeScript entry points a repository declares. A
// manifest that lists bare paths names each entry after its file.
func TsEntries(repoPath string) []TsEntry {
	data, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(TsEntriesPath)))
	if err != nil {
		return nil
	}
	declared := tsEntriesFile{}
	if err := yaml.Unmarshal(data, &declared); err == nil && len(declared.Entries) > 0 {
		out := make([]TsEntry, 0, len(declared.Entries))
		for _, entry := range declared.Entries {
			path := strings.TrimPrefix(strings.TrimSpace(entry.Path), "./")
			if path == "" {
				continue
			}
			name := entry.Name
			if name == "" {
				name = entryName(path)
			}
			out = append(out, TsEntry{Name: name, Path: path})
		}
		if len(out) > 0 {
			sortEntries(out)
			return out
		}
	}
	paths := []string{}
	if err := yaml.Unmarshal(data, &paths); err != nil {
		return nil
	}
	out := make([]TsEntry, 0, len(paths))
	for _, declared := range paths {
		path := strings.TrimPrefix(strings.TrimSpace(declared), "./")
		if path == "" {
			continue
		}
		out = append(out, TsEntry{Name: entryName(path), Path: path})
	}
	sortEntries(out)
	return out
}

func sortEntries(entries []TsEntry) {
	sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
}

func entryName(file string) string {
	base := strings.TrimSuffix(path.Base(file), path.Ext(file))
	if base == "mod" || base == "index" || base == "" {
		base = path.Base(path.Dir(file))
	}
	return base
}

// GoCommandDirs lists the Go commands of a checkout: every directory under
// cmd/ whose package is main.
func GoCommandDirs(repoPath string, files []specsync.SourceFile) []string {
	byDirectory := map[string][]string{}
	for _, file := range files {
		directory := path.Dir(file.Path)
		if directory != "cmd" && !strings.HasPrefix(directory, "cmd/") {
			continue
		}
		if path.Ext(file.Path) != ".go" || specsync.IsTestFile(file.Path) {
			continue
		}
		byDirectory[directory] = append(byDirectory[directory], file.Path)
	}
	out := make([]string, 0, len(byDirectory))
	for directory, sources := range byDirectory {
		sort.Strings(sources)
		for _, source := range sources {
			if isMainPackage(filepath.Join(repoPath, filepath.FromSlash(source))) {
				out = append(out, directory)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func isMainPackage(file string) bool {
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		declared, ok := strings.CutPrefix(strings.TrimSpace(line), "package ")
		if !ok {
			continue
		}
		return strings.TrimSpace(declared) == "main"
	}
	return false
}

// applicationEdges keeps a whole command out of the dependency set of a
// package and reports the orchestrator every partition an application entry
// point imports carries: the entry point itself unless another one named it
// first.
func applicationEdges(partitions []specsync.Partition, entryPoints []string) ([]specsync.Partition, map[string]string) {
	if len(entryPoints) == 0 {
		return partitions, nil
	}
	entry := map[string]bool{}
	for _, name := range entryPoints {
		entry[name] = true
	}
	orchestrators := map[string]string{}
	for index := range partitions {
		partition := &partitions[index]
		if entry[partition.Name] {
			for _, target := range partition.DependsOn {
				if entry[target] {
					continue
				}
				if _, named := orchestrators[target]; !named {
					orchestrators[target] = spec.RefPrefixContext + partition.Name
				}
			}
			continue
		}
		kept := make([]string, 0, len(partition.DependsOn))
		for _, target := range partition.DependsOn {
			if entry[target] {
				continue
			}
			kept = append(kept, target)
		}
		partition.DependsOn = kept
	}
	return partitions, orchestrators
}

// ApplicationDirectories lists the application entry points of a checkout: the
// Go commands and the directories the TypeScript entries start in, each once
// and sorted.
func ApplicationDirectories(repoPath string, files []specsync.SourceFile) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, directory := range GoCommandDirs(repoPath, files) {
		if seen[directory] {
			continue
		}
		seen[directory] = true
		out = append(out, directory)
	}
	for _, entry := range TsEntries(repoPath) {
		directory := path.Dir(entry.Path)
		if directory == "" {
			directory = "."
		}
		if seen[directory] {
			continue
		}
		seen[directory] = true
		out = append(out, directory)
	}
	sort.Strings(out)
	return out
}

// declaredEntryNames maps the directory a TypeScript entry starts in onto the
// name the manifest gives that entry, so its partition takes the entry's name
// rather than the directory's.
func declaredEntryNames(repoPath string) map[string]string {
	out := map[string]string{}
	for _, entry := range TsEntries(repoPath) {
		directory := path.Dir(entry.Path)
		if directory == "" {
			directory = "."
		}
		if entry.Name == "" {
			continue
		}
		if _, taken := out[directory]; taken {
			continue
		}
		out[directory] = entry.Name
	}
	return out
}

// ApplicationPartitions makes one partition for every application entry point
// of a checkout, beside the partitions the tree already made, and reports the
// names of those partitions. An entry point is an orchestrator: its own files
// leave the partition that held them, and it dependsOn the partitions its
// production imports resolve to. Nothing depends on an entry point.
func ApplicationPartitions(
	repoPath string,
	files []specsync.SourceFile,
	symbols []specsync.Symbol,
	partitions []specsync.Partition,
	repositoryName string,
) ([]specsync.Partition, []string) {
	directories := ApplicationDirectories(repoPath, files)
	if len(directories) == 0 {
		return partitions, nil
	}
	owned := map[string]bool{}
	for _, directory := range directories {
		owned[directory] = true
	}
	commands := map[string]bool{}
	for _, directory := range GoCommandDirs(repoPath, files) {
		commands[directory] = true
	}
	used := map[string]int{}
	for _, partition := range partitions {
		used[partition.Name]++
	}
	declared := declaredEntryNames(repoPath)
	names := map[string]string{}
	namesOf := func(directory string) string {
		if name, ok := names[directory]; ok {
			return name
		}
		name := specsync.PartitionName(directory, repositoryName)
		if entry, ok := declared[directory]; ok && !commands[directory] {
			name = sanitizeName(entry)
		}
		name = specsync.UniqueName(name, used)
		names[directory] = name
		return name
	}

	present := map[string]bool{}
	out := make([]specsync.Partition, 0, len(partitions)+len(directories))
	for _, partition := range partitions {
		if owned[partition.Directory] {
			names[partition.Directory] = partition.Name
			present[partition.Directory] = true
			out = append(out, partition)
			continue
		}
		kept := make([]string, 0, len(partition.Files))
		for _, file := range partition.Files {
			if applicationOwner(file, directories) != "" {
				continue
			}
			kept = append(kept, file)
		}
		partition.Files = kept
		if len(partition.Files) == 0 && len(partition.TreeFiles) == 0 {
			continue
		}
		out = append(out, partition)
	}

	entryNames := make([]string, 0, len(directories))
	for _, directory := range directories {
		name := namesOf(directory)
		entryNames = append(entryNames, name)
		if present[directory] {
			continue
		}
		entry := specsync.Partition{Directory: directory, Files: filesUnder(files, directory), Name: name}
		entry.Symbols = specsync.SymbolsUnder(entry, symbols)
		out = append(out, entry)
	}
	sort.SliceStable(out, func(left, right int) bool { return out[left].Directory < out[right].Directory })
	sort.Strings(entryNames)
	return out, entryNames
}

func filesUnder(files []specsync.SourceFile, directory string) []string {
	out := []string{}
	for _, file := range files {
		if applicationOwner(file.Path, []string{directory}) != "" {
			out = append(out, file.Path)
		}
	}
	sort.Strings(out)
	return out
}

// applicationOwner reports the entry point a path belongs to: the longest
// application directory the path sits in, or nothing.
func applicationOwner(file string, directories []string) string {
	best := ""
	for _, directory := range directories {
		if directory == "." {
			if path.Dir(file) == "." && best == "" {
				best = directory
			}
			continue
		}
		if file == directory || strings.HasPrefix(file, directory+"/") {
			if len(directory) > len(best) {
				best = directory
			}
		}
	}
	return best
}
