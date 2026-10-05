package specsync

import (
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/mirror"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

type SourceFile struct {
	Path     string
	Language string
}

type Symbol struct {
	ID string

	Name string

	Qualified string

	Kind      string
	Signature string
	File      string
	Line      int
	Exported  bool
}

func InterfaceKey(symbol Symbol) string {
	switch symbol.Kind {
	case "method", "constructor":
		if symbol.Qualified != "" {
			return symbol.Qualified
		}
	}
	return symbol.Name
}

type Import struct {
	From string

	Path string
}

type Facts struct {
	Commit string

	Files   []SourceFile
	Symbols []Symbol

	Imports []Import
}

type Partition struct {
	Name      string
	Directory string

	Files     []string
	TreeFiles []string
	Symbols   []Symbol

	DependsOn []string
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
	case strings.HasSuffix(base, "_test.ts"), strings.HasSuffix(base, "_test.tsx"):
		return true
	case strings.HasSuffix(base, "_test.js"), strings.HasSuffix(base, "_test.mjs"):
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

type PartitionOptions struct {
	Mode string

	Include []string

	Exclude []string

	Roots []string

	RepositoryName string

	TreeFiles []string

	ModulePath string

	ImportMap map[string]string

	Generated map[string]bool

	RootDirs []string

	RootContext bool
}

var DefaultRootDirs = []string{"docs"}

func PartitionFacts(facts Facts, repositoryName string) []Partition {
	return PartitionFactsWith(facts, PartitionOptions{RepositoryName: repositoryName})
}

func PartitionFactsWith(facts Facts, options PartitionOptions) []Partition {
	files := filterFiles(facts.Files, options.Include, options.Exclude)
	roots := options.Roots
	if options.Mode == spec.PartitionPackage && len(roots) == 0 {
		roots = []string{"."}
	}
	if options.Mode != spec.PartitionPackage {
		roots = nil
	}
	options.Roots = roots
	partitions := partitionsByKey(files, facts.Symbols, options)
	return attachTreeFiles(partitions, options.TreeFiles, options)
}

func rootDirs(options PartitionOptions) []string {
	if len(options.RootDirs) > 0 {
		return options.RootDirs
	}
	return DefaultRootDirs
}

func partitionsByKey(files []SourceFile, symbols []Symbol, options PartitionOptions) []Partition {
	byDirectory := map[string]*Partition{}
	order := []string{}
	for _, file := range files {
		directory := path.Dir(file.Path)
		if directory == "" {
			directory = "."
		}
		directory = foldGenerated(directory, options.Roots, options.Generated)
		if options.Mode == spec.PartitionPackage && options.Roots != nil {
			directory = enclosingRoot(directory, options.Roots)
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
		partition.Name = uniqueName(partitionName(directory, options.RepositoryName), used)
		partitions = append(partitions, *partition)
	}
	for index := range partitions {
		partitions[index].Symbols = symbolsUnder(partitions[index], symbols)
	}
	return partitions
}

func attachTreeFiles(partitions []Partition, treeFiles []string, options PartitionOptions) []Partition {
	if len(treeFiles) == 0 {
		return partitions
	}
	byDirectory := map[string]int{}
	for index := range partitions {
		byDirectory[partitions[index].Directory] = index
	}
	owner := map[string]int{}
	root := -1
	for _, file := range treeFiles {
		if IsLegacySpecMirrorPath(file) || file == "" {
			continue
		}
		target := treeFileDirectory(file, byDirectory, options)
		if target == "" {
			continue
		}
		index, ok := byDirectory[target]
		if !ok {
			if root < 0 {
				root = len(partitions)
				partitions = append(partitions, Partition{Directory: target})
			}
			index = root
		}
		owner[file] = index
	}
	used := map[string]int{}
	for index := range partitions {
		used[partitions[index].Name]++
	}
	for _, file := range treeFiles {
		index, ok := owner[file]
		if !ok {
			continue
		}
		if partitions[index].Directory == "." && partitions[index].Name == "" {
			name := partitionName(".", options.RepositoryName)
			if used[name] > 0 {
				name = name + "-root"
			}
			partitions[index].Name = uniqueName(name, used)
		}
		if slices.Contains(partitions[index].Files, file) || slices.Contains(partitions[index].TreeFiles, file) {
			continue
		}
		partitions[index].TreeFiles = append(partitions[index].TreeFiles, file)
	}
	for index := range partitions {
		sort.Strings(partitions[index].TreeFiles)
	}
	sort.SliceStable(partitions, func(left, right int) bool {
		return partitions[left].Directory < partitions[right].Directory
	})
	return partitions
}

func treeFileDirectory(file string, byDirectory map[string]int, options PartitionOptions) string {
	directory := path.Dir(file)
	if directory == "" {
		directory = "."
	}
	directory = foldGenerated(directory, options.Roots, options.Generated)
	roots := options.Roots
	if options.Mode != spec.PartitionPackage {
		roots = nil
	}
	if roots != nil {
		directory = enclosingRoot(directory, roots)
	}
	best := ""
	for candidate := range byDirectory {
		if directory == candidate || strings.HasPrefix(directory, candidate+"/") {
			if len(candidate) > len(best) {
				best = candidate
			}
		}
	}
	if best != "" {
		return best
	}
	if options.RootContext && isRootFile(file, rootDirs(options)) {
		return "."
	}
	return ""
}

func isRootFile(file string, rootDirs []string) bool {
	if !strings.Contains(file, "/") {
		return true
	}
	for _, dir := range rootDirs {
		if dir != "" && strings.HasPrefix(file, dir+"/") {
			return true
		}
	}
	return false
}

var PackageManifests = []string{
	"go.mod", "go.work", "deno.json", "deno.jsonc", "package.json",
	"Cargo.toml", "pyproject.toml", "setup.py", "pom.xml", "build.gradle",
	"Gemfile", "composer.json",
}

func enclosingRoot(directory string, roots []string) string {
	best := "."
	for _, root := range roots {
		if root == "." || directory == root || strings.HasPrefix(directory, root+"/") {
			if len(root) > len(best) {
				best = root
			}
		}
	}
	return best
}

// foldGenerated maps a directory that holds only generated code onto the
// nearest ancestor that is not generated and not a package root, so a codegen
// tree such as an @atproto/lex lexicon output never becomes a context of its
// own.
func foldGenerated(directory string, roots []string, generated map[string]bool) string {
	for directory != "." && directory != "" && generated[directory] && !slices.Contains(roots, directory) {
		directory = path.Dir(directory)
		if directory == "" {
			directory = "."
		}
	}
	return directory
}

func filterFiles(files []SourceFile, include, exclude []string) []SourceFile {
	includes := compileGlobs(include)
	excludes := compileGlobs(exclude)
	out := make([]SourceFile, 0, len(files))
	for _, file := range files {
		if IsLegacySpecMirrorPath(file.Path) {
			continue
		}
		if len(includes) > 0 && !matchAny(includes, file.Path) {
			continue
		}
		if matchAny(excludes, file.Path) {
			continue
		}
		out = append(out, file)
	}
	return out
}

func IsLegacySpecMirrorPath(file string) bool {
	return file == mirror.LegacyDir || strings.HasPrefix(file, mirror.LegacyDir+"/")
}

func MatchGlob(pattern, name string) bool {
	return matchAny(compileGlobs([]string{pattern}), name)
}

type glob struct {
	full *regexp.Regexp

	base *regexp.Regexp
}

func compileGlobs(patterns []string) []glob {
	out := make([]glob, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		compiled := glob{full: regexp.MustCompile(globRegexp(pattern))}
		if !strings.Contains(pattern, "/") {
			compiled.base = regexp.MustCompile(globRegexp(pattern))
		}
		out = append(out, compiled)
	}
	return out
}

func matchAny(globs []glob, name string) bool {
	for _, compiled := range globs {
		if compiled.full.MatchString(name) {
			return true
		}
		if compiled.base != nil && compiled.base.MatchString(path.Base(name)) {
			return true
		}
	}
	return false
}

func globRegexp(pattern string) string {
	builder := strings.Builder{}
	builder.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		switch char := pattern[index]; char {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				if index+2 < len(pattern) && pattern[index+2] == '/' {
					builder.WriteString("(?:.*/)?")
					index += 2
					continue
				}
				builder.WriteString(".*")
				index++
				continue
			}
			builder.WriteString("[^/]*")
		case '?':
			builder.WriteString("[^/]")
		default:
			builder.WriteString(regexp.QuoteMeta(string(char)))
		}
	}
	builder.WriteString("$")
	return builder.String()
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

var generatedFilePrefixes = []string{"zz_generated", "zz_gen"}

var generatedSymbolPrefixes = []string{"DeepCopy"}

func IsGeneratedSymbol(file, name string) bool {
	base := path.Base(file)
	for _, prefix := range generatedFilePrefixes {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	for _, prefix := range generatedSymbolPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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
		if IsGeneratedSymbol(symbol.File, symbol.Name) || IsGeneratedSymbol(symbol.File, InterfaceKey(symbol)) {
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

const dependencyRoot = "."

// ModuleResolver turns an import specifier into the repository-relative path
// it names. Go resolves through the module path; TypeScript resolves a relative
// import against the importing file and a bare specifier through the deno.json
// import map and the workspace member names.
type ModuleResolver struct {
	ModulePath string

	ImportMap map[string]string
}

func (r ModuleResolver) Resolve(from, specifier string) (string, bool) {
	if specifier == "" {
		return "", false
	}
	if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") || specifier == "." || specifier == ".." {
		return path.Clean(path.Join(path.Dir(from), specifier)), true
	}
	if r.ModulePath != "" {
		if directory, ok := importDirectory(specifier, r.ModulePath); ok {
			return directory, true
		}
	}
	return r.fromImportMap(specifier)
}

func (r ModuleResolver) fromImportMap(specifier string) (string, bool) {
	best := ""
	for key := range r.ImportMap {
		trimmed := strings.TrimSuffix(key, "/")
		if specifier == trimmed || strings.HasPrefix(specifier, trimmed+"/") {
			if len(key) > len(best) {
				best = key
			}
		}
	}
	if best == "" {
		return "", false
	}
	target := r.ImportMap[best]
	if !strings.HasPrefix(target, "./") && !strings.HasPrefix(target, "../") {
		return "", false
	}
	trimmed := strings.TrimSuffix(best, "/")
	rest := strings.TrimPrefix(strings.TrimPrefix(specifier, trimmed), "/")
	joined := strings.TrimPrefix(target, "./")
	if rest != "" {
		joined = path.Join(joined, rest)
	}
	return path.Clean(joined), true
}

func PartitionDependencies(partitions []Partition, imports []Import, resolver ModuleResolver) map[string][]string {
	byDirectory := map[string]string{}
	for _, partition := range partitions {
		byDirectory[partition.Directory] = partition.Name
	}
	owner := map[string]string{}
	for _, partition := range partitions {
		for _, file := range partition.Files {
			owner[file] = partition.Name
		}
	}
	dependencies := map[string]map[string]bool{}
	for _, partition := range partitions {
		dependencies[partition.Name] = map[string]bool{}
	}
	for _, edge := range imports {
		from, ok := owner[edge.From]
		if !ok {
			continue
		}
		resolved, ok := resolver.Resolve(edge.From, edge.Path)
		if !ok {
			continue
		}
		target, ok := partitionForPath(resolved, byDirectory)
		if !ok || target == from {
			continue
		}
		dependencies[from][target] = true
	}
	root := byDirectory[dependencyRoot]
	for _, partition := range partitions {
		if root != "" && partition.Name != root {
			dependencies[partition.Name][root] = true
		}
	}
	out := map[string][]string{}
	for name, targets := range dependencies {
		ordered := make([]string, 0, len(targets))
		for target := range targets {
			ordered = append(ordered, target)
		}
		sort.Strings(ordered)
		if len(ordered) == 0 {
			continue
		}
		out[name] = ordered
	}
	return out
}

func partitionForPath(resolved string, byDirectory map[string]string) (string, bool) {
	if resolved == "" || strings.HasPrefix(resolved, "..") {
		return "", false
	}
	best := ""
	for candidate := range byDirectory {
		if resolved == candidate || strings.HasPrefix(resolved, candidate+"/") {
			if len(candidate) > len(best) {
				best = candidate
			}
		}
	}
	if best == "" {
		return "", false
	}
	return byDirectory[best], true
}

func importDirectory(importPath, modulePath string) (string, bool) {
	if modulePath == "" || importPath == "" {
		return "", false
	}
	trimmed := strings.TrimSuffix(importPath, "/")
	if trimmed == modulePath {
		return ".", true
	}
	relative, ok := strings.CutPrefix(trimmed, modulePath+"/")
	if !ok || relative == "" {
		return "", false
	}
	return relative, true
}

func DependencyRefs(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, spec.RefPrefixContext+name)
	}
	sort.Strings(out)
	return out
}

func Observed(partition Partition) spec.ObservedFacts {
	files := append([]string{}, partition.Files...)
	sort.Strings(files)
	files = dedupe(files)
	treeFiles := append([]string{}, partition.TreeFiles...)
	sort.Strings(treeFiles)
	treeFiles = dedupe(treeFiles)

	interfaces := make([]spec.ObservedInterface, 0, len(partition.Symbols))
	keyed := map[string]bool{}
	for _, symbol := range partition.Symbols {
		key := InterfaceKey(symbol)
		if keyed[key] {
			continue
		}
		keyed[key] = true
		interfaces = append(interfaces, spec.ObservedInterface{
			Name:        key,
			Kind:        symbol.Kind,
			Signature:   symbol.Signature,
			File:        symbol.File,
			Line:        symbol.Line,
			CodegraphID: symbol.ID,
		})
	}
	observed := spec.ObservedFacts{Files: files, TreeFiles: treeFiles, Interfaces: interfaces}
	observed.Fingerprint = Fingerprint(observed)
	return observed
}

func QualifiedRewrites(observed spec.ObservedFacts) map[string]string {
	exact := map[string]bool{}
	for _, entry := range observed.Interfaces {
		exact[entry.Name] = true
	}
	counts := map[string]int{}
	keys := map[string]string{}
	for _, entry := range observed.Interfaces {
		index := strings.LastIndex(entry.Name, ".")
		if index < 0 || index == len(entry.Name)-1 {
			continue
		}
		bare := entry.Name[index+1:]
		counts[bare]++
		keys[bare] = entry.Name
	}
	out := map[string]string{}
	for bare, count := range counts {
		if count == 1 && !exact[bare] {
			out[bare] = keys[bare]
		}
	}
	return out
}

func MigrateDeclared(declared spec.SystemContextSpec, observed spec.ObservedFacts) spec.SystemContextSpec {
	rewrites := QualifiedRewrites(observed)
	if len(rewrites) == 0 {
		return declared
	}
	out := declared
	interfaces := declared.Interfaces
	copiedInterfaces := false
	for index, entry := range declared.Interfaces {
		key, ok := rewrites[entry.Name]
		if !ok {
			continue
		}
		if !copiedInterfaces {
			interfaces = append([]spec.Interface{}, declared.Interfaces...)
			copiedInterfaces = true
		}
		interfaces[index].Name = key
	}
	if copiedInterfaces {
		out.Interfaces = interfaces
	}

	requirements := declared.Requirements
	copiedRequirements := false
	for index, requirement := range declared.Requirements {
		refs := requirement.CodeRefs
		changed := false
		for refIndex, ref := range requirement.CodeRefs {
			qualified, ok := qualifiedRef(ref, rewrites)
			if !ok {
				continue
			}
			if !changed {
				refs = append([]string{}, requirement.CodeRefs...)
				changed = true
			}
			refs[refIndex] = qualified
		}
		if !changed {
			continue
		}
		if !copiedRequirements {
			requirements = append([]spec.Requirement{}, declared.Requirements...)
			copiedRequirements = true
		}
		requirements[index].CodeRefs = refs
	}
	if copiedRequirements {
		out.Requirements = requirements
	}
	return out
}

func qualifiedRef(ref string, rewrites map[string]string) (string, bool) {
	if key, ok := rewrites[ref]; ok {
		return key, true
	}
	for _, prefix := range spec.CodeRefPrefixes {
		bare, ok := strings.CutPrefix(ref, prefix)
		if !ok {
			continue
		}
		if key, ok := rewrites[bare]; ok {
			return prefix + key, true
		}
		return "", false
	}
	return "", false
}

// ReanchorRefs rewrites a code ref that names a symbol by its codegraph id when
// that id moved: the ids hash the symbol's place in the file, so an edit that
// inserts lines above a symbol changes every id below it. The symbol name the
// previous facts recorded for the old id finds the new one, so requirements the
// edit did not touch stay resolvable.
func ReanchorRefs(declared spec.SystemContextSpec, previous, observed spec.ObservedFacts) spec.SystemContextSpec {
	moved := movedIDs(previous, observed)
	if len(moved) == 0 {
		return declared
	}
	out := declared
	if refs := rewriteRefs(declared.CodeRefs, moved); refs != nil {
		out.CodeRefs = refs
	}
	requirements := declared.Requirements
	copied := false
	for index, requirement := range declared.Requirements {
		refs := rewriteRefs(requirement.CodeRefs, moved)
		if refs == nil {
			continue
		}
		if !copied {
			requirements = append([]spec.Requirement{}, declared.Requirements...)
			copied = true
		}
		requirements[index].CodeRefs = refs
	}
	if copied {
		out.Requirements = requirements
	}
	return out
}

func movedIDs(previous, observed spec.ObservedFacts) map[string]string {
	byName := map[string]string{}
	counts := map[string]int{}
	live := map[string]bool{}
	for _, entry := range observed.Interfaces {
		if entry.CodegraphID != "" {
			live[entry.CodegraphID] = true
		}
		if entry.Name == "" || entry.CodegraphID == "" {
			continue
		}
		counts[entry.Name]++
		if _, seen := byName[entry.Name]; !seen {
			byName[entry.Name] = entry.CodegraphID
		}
	}
	out := map[string]string{}
	for _, entry := range previous.Interfaces {
		if entry.CodegraphID == "" || entry.Name == "" || live[entry.CodegraphID] {
			continue
		}
		if counts[entry.Name] != 1 {
			continue
		}
		if next, ok := byName[entry.Name]; ok && next != entry.CodegraphID {
			out[entry.CodegraphID] = next
		}
	}
	return out
}

func rewriteRefs(refs []string, moved map[string]string) []string {
	var out []string
	for index, ref := range refs {
		next, ok := moved[ref]
		if !ok {
			continue
		}
		if out == nil {
			out = append([]string{}, refs...)
		}
		out[index] = next
	}
	return out
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
	SpecValid bool

	ValidatorMessage string

	CodeSynced bool

	Missing []string

	Undeclared []string

	Unresolved []string

	Drifted bool
}

type Input struct {
	Name string

	Generation int64

	Spec spec.SystemContextSpec

	Observed spec.ObservedFacts

	SyncedFingerprint string
}

func Decide(in Input) Decision {
	decision := Decision{SpecValid: true}
	candidate := spec.SystemContext{ObjectMeta: metav1.ObjectMeta{Name: in.Name}, Spec: in.Spec}
	if result := spec.ValidateSystemContext(&candidate); !result.OK() {
		decision.SpecValid = false
		decision.ValidatorMessage = result.Err().Error()
	}

	declaredNames := map[string]bool{}
	for _, declaredInterface := range in.Spec.Interfaces {
		declaredNames[declaredInterface.Name] = true
	}
	observedNames := map[string]bool{}
	for _, observedInterface := range in.Observed.Interfaces {
		observedNames[observedInterface.Name] = true
	}

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

	decision.Unresolved = UnresolvedCodeRefs(in.Spec.Requirements, in.Observed)
	decision.CodeSynced = len(decision.Missing) == 0 && len(decision.Undeclared) == 0 && len(decision.Unresolved) == 0
	decision.Drifted = in.SyncedFingerprint != "" && in.SyncedFingerprint != in.Observed.Fingerprint
	return decision
}

func CodeToSpecDue(drifted bool, fromCommit, toCommit string) bool {
	return drifted && fromCommit != "" && toCommit != "" && fromCommit != toCommit
}

func SpecEditDue(specHash, realizedSpecHash, originHash string) bool {
	if realizedSpecHash == "" || specHash == "" || specHash == realizedSpecHash {
		return false
	}
	return specHash != originHash
}

func RetryBackoff(attempts int, lastAttempt, now time.Time, base time.Duration, maxAttempts int) (time.Duration, bool) {
	if maxAttempts > 0 && attempts >= maxAttempts {
		return 0, false
	}
	if base <= 0 || lastAttempt.IsZero() || attempts == 0 {
		return 0, true
	}
	wait := base
	for count := 1; count < attempts && wait < maxRetryBackoff; count++ {
		wait *= 2
	}
	if now.Sub(lastAttempt) >= wait {
		return 0, true
	}
	return wait - now.Sub(lastAttempt), true
}

const maxRetryBackoff = 10 * time.Minute

func RunningAdmitted(name string, running []string) bool {
	for _, other := range running {
		if other != name && other < name {
			return false
		}
	}
	return true
}

func ResolvableRefs(observed spec.ObservedFacts) map[string]bool {
	resolvable := map[string]bool{}
	for _, file := range append(append([]string{}, observed.Files...), observed.TreeFiles...) {
		resolvable[spec.CodeRefPrefixFile+file] = true
	}
	for _, observedInterface := range observed.Interfaces {
		if observedInterface.CodegraphID != "" {
			resolvable[observedInterface.CodegraphID] = true
		}
		resolvable[observedInterface.Name] = true
		for _, prefix := range spec.CodeRefPrefixes {
			resolvable[prefix+observedInterface.Name] = true
		}
	}
	return resolvable
}

func UnresolvedCodeRefs(requirements []spec.Requirement, observed spec.ObservedFacts) []string {
	resolvable := ResolvableRefs(observed)

	unresolved := []string{}
	seen := map[string]bool{}
	for _, requirement := range requirements {
		for _, ref := range requirement.CodeRefs {
			if resolvable[ref] || seen[ref] {
				continue
			}
			seen[ref] = true
			unresolved = append(unresolved, requirement.ID+": "+ref)
		}
	}
	sort.Strings(unresolved)
	return unresolved
}

func Conditions(in Input, previous []metav1.Condition) (Decision, []metav1.Condition) {
	decision := Decide(in)
	// Start from every condition the object already carries, so a condition
	// another reconciler owns (PolicyCompliant, AcceptanceOverridden) survives
	// this one. Set replaces the three this function owns in place.
	out := condition.Copy(previous)

	if decision.SpecValid {
		condition.SetTrue(&out, in.Generation, specapi.ConditionSpecValid,
			specapi.ReasonValidatorPassed, "the spec passes the validator")
	} else {
		condition.SetFalse(&out, in.Generation, specapi.ConditionSpecValid,
			specapi.ReasonValidatorFailed, decision.ValidatorMessage)
	}

	switch {
	case decision.CodeSynced:
		condition.SetTrue(&out, in.Generation, specapi.ConditionCodeSynced,
			specapi.ReasonInterfacesObserved,
			"every declared interface is observed, nothing undeclared is exported and every requirement code ref resolves")
	case len(decision.Missing) > 0 || len(decision.Undeclared) > 0:
		condition.SetFalse(&out, in.Generation, specapi.ConditionCodeSynced,
			specapi.ReasonInterfacesMissing, syncMessage(decision))
	default:
		condition.SetFalse(&out, in.Generation, specapi.ConditionCodeSynced,
			specapi.ReasonCodeRefsUnresolved, "unresolved requirement code refs: "+strings.Join(decision.Unresolved, ", "))
	}

	switch {
	case decision.Drifted:
		condition.SetTrue(&out, in.Generation, specapi.ConditionDrifted,
			specapi.ReasonFingerprintChanged, "observed facts changed since the spec was last synced")
	case in.SyncedFingerprint == "":
		condition.SetFalse(&out, in.Generation, specapi.ConditionDrifted,
			specapi.ReasonNotSyncedYet, "no synced fingerprint has been recorded yet")
	default:
		condition.SetFalse(&out, in.Generation, specapi.ConditionDrifted,
			specapi.ReasonFingerprintEqual, "observed facts match the synced fingerprint")
	}
	return decision, out
}

func syncMessage(decision Decision) string {
	parts := []string{}
	if len(decision.Missing) > 0 {
		parts = append(parts, "missing: "+strings.Join(decision.Missing, ", "))
	}
	if len(decision.Undeclared) > 0 {
		parts = append(parts, "undeclared: "+strings.Join(decision.Undeclared, ", "))
	}
	if len(decision.Unresolved) > 0 {
		parts = append(parts, "unresolved: "+strings.Join(decision.Unresolved, ", "))
	}
	return strings.Join(parts, "; ")
}
