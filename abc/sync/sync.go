package specsync

import (
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

type SourceFile struct {
	Path     string
	Language string
}

type Symbol struct {
	ID        string
	Name      string
	Kind      string
	Signature string
	File      string
	Line      int
	Exported  bool
}

type Facts struct {
	Commit  string
	Files   []SourceFile
	Symbols []Symbol
}

type Partition struct {
	Name      string
	Directory string
	Files     []string
	Symbols   []Symbol
}

var interfaceKinds = map[string]bool{
	"function":    true,
	"method":      true,
	"constructor": true,
	"struct":      true,
	"interface":   true,
	"class":       true,
	"type_alias":  true,
	"enum":        true,
}

func IsInterfaceKind(kind string) bool {
	return interfaceKinds[kind]
}

func IsTestFile(file string) bool {
	base := path.Base(file)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".test.tsx"):
		return true
	case strings.HasSuffix(base, ".spec.ts"), strings.HasSuffix(base, ".spec.tsx"):
		return true
	case strings.HasSuffix(base, ".test.js"), strings.HasSuffix(base, ".spec.js"):
		return true
	}
	return false
}

// PartitionFacts splits the facts into one partition per directory that holds
// a source file. The name is the directory path with slashes turned into
// dashes, which is a DNS-1123 label; root files take the repository name.
func PartitionFacts(facts Facts, repositoryName string) []Partition {
	byDirectory := map[string]*Partition{}
	order := []string{}
	for _, file := range facts.Files {
		directory := path.Dir(file.Path)
		if directory == "" {
			directory = "."
		}
		partition, ok := byDirectory[directory]
		if !ok {
			partition = &Partition{Directory: directory}
			byDirectory[directory] = partition
			order = append(order, directory)
		}
		partition.Files = append(partition.Files, file.Path)
	}
	sort.Strings(order)

	partitions := make([]Partition, 0, len(order))
	used := map[string]int{}
	for _, directory := range order {
		partition := byDirectory[directory]
		sort.Strings(partition.Files)
		partition.Name = uniqueName(partitionName(directory, repositoryName), used)
		partitions = append(partitions, *partition)
	}
	for index := range partitions {
		partitions[index].Symbols = symbolsUnder(partitions[index], facts.Symbols)
	}
	return partitions
}

func partitionName(directory, repositoryName string) string {
	if directory == "." {
		return sanitizeName(repositoryName)
	}
	return sanitizeName(strings.ReplaceAll(directory, "/", "-"))
}

func sanitizeName(value string) string {
	lowered := strings.ToLower(value)
	builder := strings.Builder{}
	lastDash := false
	for _, char := range lowered {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
			lastDash = false
		case char == '-' || char == '_' || char == '.':
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		name = "context"
	}
	return name
}

func uniqueName(name string, used map[string]int) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	for suffix := used[name]; ; suffix++ {
		candidate := name + "-" + strconv.Itoa(suffix)
		if used[candidate] == 0 {
			used[candidate]++
			return candidate
		}
	}
}

func symbolsUnder(partition Partition, symbols []Symbol) []Symbol {
	inPartition := map[string]bool{}
	for _, file := range partition.Files {
		inPartition[file] = true
	}
	out := []Symbol{}
	seen := map[string]bool{}
	for _, symbol := range symbols {
		if !inPartition[symbol.File] || IsTestFile(symbol.File) {
			continue
		}
		if !symbol.Exported || !IsInterfaceKind(symbol.Kind) {
			continue
		}
		if seen[symbol.ID] {
			continue
		}
		seen[symbol.ID] = true
		out = append(out, symbol)
	}
	sort.Slice(out, func(left, right int) bool {
		if out[left].File != out[right].File {
			return out[left].File < out[right].File
		}
		if out[left].Line != out[right].Line {
			return out[left].Line < out[right].Line
		}
		if out[left].Name != out[right].Name {
			return out[left].Name < out[right].Name
		}
		return out[left].ID < out[right].ID
	})
	return out
}

// Observed turns a partition into the deterministic status.observed block.
// The fingerprint covers the files and the interfaces, sorted, so two runs
// over the same tree produce the same digest.
func Observed(partition Partition) spec.ObservedFacts {
	files := append([]string{}, partition.Files...)
	sort.Strings(files)
	files = dedupe(files)

	interfaces := make([]spec.ObservedInterface, 0, len(partition.Symbols))
	for _, symbol := range partition.Symbols {
		interfaces = append(interfaces, spec.ObservedInterface{
			Name:        symbol.Name,
			Kind:        symbol.Kind,
			Signature:   symbol.Signature,
			File:        symbol.File,
			Line:        symbol.Line,
			CodegraphID: symbol.ID,
		})
	}
	observed := spec.ObservedFacts{Files: files, Interfaces: interfaces}
	observed.Fingerprint = Fingerprint(observed)
	return observed
}

type fingerprintPayload struct {
	Files      []string                 `json:"files"`
	Interfaces []spec.ObservedInterface `json:"interfaces"`
}

func Fingerprint(observed spec.ObservedFacts) string {
	files := append([]string{}, observed.Files...)
	sort.Strings(files)
	interfaces := append([]spec.ObservedInterface{}, observed.Interfaces...)
	sort.Slice(interfaces, func(left, right int) bool {
		if interfaces[left].File != interfaces[right].File {
			return interfaces[left].File < interfaces[right].File
		}
		if interfaces[left].Line != interfaces[right].Line {
			return interfaces[left].Line < interfaces[right].Line
		}
		if interfaces[left].Name != interfaces[right].Name {
			return interfaces[left].Name < interfaces[right].Name
		}
		return interfaces[left].CodegraphID < interfaces[right].CodegraphID
	})
	hash, err := specapi.HashJSON(fingerprintPayload{Files: files, Interfaces: interfaces})
	if err != nil {
		panic(err)
	}
	return hash
}

func dedupe(values []string) []string {
	out := values[:0]
	for index, value := range values {
		if index > 0 && values[index-1] == value {
			continue
		}
		out = append(out, value)
	}
	return out
}

type Decision struct {
	Drifted bool

	CodeSynced bool

	Missing    []string
	Undeclared []string
}

// Decide compares the declared interface surface with the observed one and the
// new fingerprint with the one the last ingest stored.
func Decide(declared []spec.Interface, observed spec.ObservedFacts, previousFingerprint string) Decision {
	declaredNames := map[string]bool{}
	for _, declaredInterface := range declared {
		declaredNames[declaredInterface.Name] = true
	}
	observedNames := map[string]bool{}
	for _, observedInterface := range observed.Interfaces {
		observedNames[observedInterface.Name] = true
	}

	decision := Decision{}
	for name := range declaredNames {
		if !observedNames[name] {
			decision.Missing = append(decision.Missing, name)
		}
	}
	for name := range observedNames {
		if !declaredNames[name] {
			decision.Undeclared = append(decision.Undeclared, name)
		}
	}
	sort.Strings(decision.Missing)
	sort.Strings(decision.Undeclared)
	decision.CodeSynced = len(decision.Missing) == 0 && len(decision.Undeclared) == 0
	decision.Drifted = previousFingerprint != "" && previousFingerprint != observed.Fingerprint
	return decision
}
