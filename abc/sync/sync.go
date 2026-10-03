package specsync

import (
	"path"
	"regexp"
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

	// Qualified is the index's qualified name of the symbol: `Type.Method` for
	// a method, the bare name for a free function or a type. It is the list-map
	// key of the observed surface, because two types may both offer a method
	// named List and the declared surface, keyed by name, could hold only one
	// of them.
	Qualified string

	Kind      string
	Signature string
	File      string
	Line      int
	Exported  bool
}

// InterfaceKey is the key a symbol takes in the observed surface and in a
// spec's declared interfaces: a method is keyed by its qualified name, so two
// types that share a method name are two entries; everything else keeps its
// bare name, so a function's key reads the way it is called.
func InterfaceKey(symbol Symbol) string {
	switch symbol.Kind {
	case "method", "constructor":
		if symbol.Qualified != "" {
			return symbol.Qualified
		}
	}
	return symbol.Name
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

// PartitionOptions is how a Repository asks for its tree to be split. Mode is
// directory (one context per directory that holds a source file) or package
// (one context per package or module root); include and exclude are globs over
// the repository-relative path, applied before the split.
type PartitionOptions struct {
	Mode string

	Include []string

	Exclude []string

	// Roots are the package roots the package partition groups by. The caller
	// reads them off the working tree, because a manifest file is not indexed
	// and so is not in Facts.Files. "." is always a root; an empty list means
	// only the repository root.
	Roots []string

	RepositoryName string
}

// PartitionFacts splits the facts into one partition per directory that holds
// a source file. The name is the directory path with slashes turned into
// dashes, which is a DNS-1123 label; root files take the repository name.
func PartitionFacts(facts Facts, repositoryName string) []Partition {
	return PartitionFactsWith(facts, PartitionOptions{RepositoryName: repositoryName})
}

func PartitionFactsWith(facts Facts, options PartitionOptions) []Partition {
	files := filterFiles(facts.Files, options.Include, options.Exclude)
	switch options.Mode {
	case spec.PartitionPackage:
		roots := options.Roots
		if len(roots) == 0 {
			roots = []string{"."}
		}
		return partitionsByKey(files, facts.Symbols, options.RepositoryName, roots)
	default:
		return partitionsByKey(files, facts.Symbols, options.RepositoryName, nil)
	}
}

// partitionsByKey groups the files by directory, or by the longest package
// root above them when roots are given. Both forms produce the same Partition
// shape, so the rest of the pipeline never asks which mode produced it.
func partitionsByKey(files []SourceFile, symbols []Symbol, repositoryName string, roots []string) []Partition {
	byDirectory := map[string]*Partition{}
	order := []string{}
	for _, file := range files {
		directory := path.Dir(file.Path)
		if directory == "" {
			directory = "."
		}
		if roots != nil {
			directory = enclosingRoot(directory, roots)
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
		partitions[index].Symbols = symbolsUnder(partitions[index], symbols)
	}
	return partitions
}

// PackageManifests are the files that mark a directory as a package or module
// root. A nested one starts its own context; files under no root belong to the
// repository's own context.
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

// filterFiles applies the include and exclude globs. An empty include list
// keeps every file; an exclude always wins.
func filterFiles(files []SourceFile, include, exclude []string) []SourceFile {
	includes := compileGlobs(include)
	excludes := compileGlobs(exclude)
	out := make([]SourceFile, 0, len(files))
	for _, file := range files {
		if IsSpecArtifact(file.Path) {
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

// IsSpecArtifact reports whether a repository-relative path is spec state
// rather than code: the context documents and the `.specs/*.yaml` mirror the
// tool itself writes. They are left out of every partition and out of the
// observed facts, even when the index happened to read one, because a spec the
// tool wrote must never look like code that drifted.
func IsSpecArtifact(file string) bool {
	return file == mirror.Dir || strings.HasPrefix(file, mirror.Dir+"/")
}

// MatchGlob matches one repository-relative path against one glob. `*` and `?`
// stay inside a path segment, `**` crosses segments, and a pattern without a
// slash also matches the base name, so `*_test.go` means every test file.
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

	// The observed interfaces are keyed by InterfaceKey everywhere they are
	// written, read and diffed, so two symbols that share a key are one entry.
	// A method's key is its qualified name, so two types that both offer a
	// method named List are two describable entries; a free function and a type
	// keep their bare name. Two symbols that still share a key — two files may
	// each declare a `Section` — are one entry: a spec that carried both could
	// not be stored at all, because the API server refuses a keyed list with a
	// duplicate key. The symbols arrive sorted, so the entry kept is the first
	// one in file and line order and two runs over the same tree produce the
	// same facts.
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
	observed := spec.ObservedFacts{Files: files, Interfaces: interfaces}
	observed.Fingerprint = Fingerprint(observed)
	return observed
}

// QualifiedRewrites maps a method's bare name to the qualified key the
// observed facts now use, for the bare names they answer to exactly once. A
// name two types share is left out: nothing may guess which receiver was meant,
// and a spec that names it must be edited by a person.
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

// MigrateDeclared moves a stored spec onto the qualified keys the observed
// facts use: a declared interface that carries a method's bare name, and a
// requirement code ref that names one, take the receiver with them when the
// observed facts name exactly one candidate. A spec keyed before the receiver
// was part of the key stays usable, which is what lets an ingest run over an
// existing cluster without a rewrite of every object.
func MigrateDeclared(declared spec.SystemContextSpec, observed spec.ObservedFacts) spec.SystemContextSpec {
	rewrites := QualifiedRewrites(observed)
	if len(rewrites) == 0 {
		return declared
	}
	out := declared
	out.Interfaces = append([]spec.Interface{}, declared.Interfaces...)
	for index, entry := range out.Interfaces {
		if key, ok := rewrites[entry.Name]; ok {
			out.Interfaces[index].Name = key
		}
	}
	out.Requirements = append([]spec.Requirement{}, declared.Requirements...)
	for index, requirement := range out.Requirements {
		refs := append([]string{}, requirement.CodeRefs...)
		changed := false
		for refIndex, ref := range refs {
			if key, ok := rewrites[ref]; ok {
				refs[refIndex] = key
				changed = true
				continue
			}
			for _, prefix := range spec.CodeRefPrefixes {
				bare, ok := strings.CutPrefix(ref, prefix)
				if !ok {
					continue
				}
				if key, ok := rewrites[bare]; ok {
					refs[refIndex] = prefix + key
					changed = true
				}
				break
			}
		}
		if changed {
			out.Requirements[index].CodeRefs = refs
		}
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

// Input is everything the deciders need about one context. It is a value, so a
// caller can decide without a cluster.
type Input struct {
	Name string

	Generation int64

	Spec spec.SystemContextSpec

	Observed spec.ObservedFacts

	SyncedFingerprint string
}

// Decide is the whole SystemContext decision: the validator, the declared
// surface against the observed one, the requirement code refs against the
// observed facts, and the observed fingerprint against the one recorded when
// the spec was last synced.
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

// CodeToSpecDue reports whether a CodeToSpec change is due: the context is
// drifted and the commit pair from the synced baseline to the observed code is
// usable.
//
// It deliberately does not ask whether Drifted just turned true. Ingest
// computes the conditions and stores them in the same status write that stores
// the new fingerprint, so the controller never sees the false-to-true edge.
// The transition is therefore read as a state (drifted, with a commit pair) and
// the caller raises one change per drift episode by refusing to create a second
// change of the same direction while one is still unfinished, which is also
// what makes a controller restart safe.
func CodeToSpecDue(drifted bool, fromCommit, toCommit string) bool {
	return drifted && fromCommit != "" && toCommit != "" && fromCommit != toCommit
}

// SpecEditDue reports whether a human spec edit waits for a realize. An empty
// realized hash means the controller has no baseline yet, so nothing is due.
//
// originHash is the spec the tool itself last wrote, carried on the object.
// A tool write is a spec update followed by a status update, and a reconcile
// can run in between; when the spec still hashes to the tool's own write there
// is no human edit to realize, only a status that has not landed yet.
func SpecEditDue(specHash, realizedSpecHash, originHash string) bool {
	if realizedSpecHash == "" || specHash == "" || specHash == realizedSpecHash {
		return false
	}
	return specHash != originHash
}

// RetryBackoff decides whether the next attempt at a drift episode may be
// raised now, later, or never. attempts is how many records of the episode
// already exist, lastAttempt is when the newest one was created (zero when that
// is unknown, which allows the retry at once), base is the wait before the
// second attempt, and maxAttempts caps the episode — zero means no cap.
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

// RunningAdmitted reports whether a change may stay Running while the others of
// its context also are. The lowest name wins, so exactly one change per
// SystemContext runs and the loser is not a race between two workers.
func RunningAdmitted(name string, running []string) bool {
	for _, other := range running {
		if other != name && other < name {
			return false
		}
	}
	return true
}

// ResolvableRefs is every spelling of a code reference the observed facts
// answer to: a file ref, an interface's CodeGraph id, its bare name, and its
// name under each code ref prefix. One definition, so ingest, the controller
// and the model parser can never disagree about what resolves.
func ResolvableRefs(observed spec.ObservedFacts) map[string]bool {
	resolvable := map[string]bool{}
	for _, file := range observed.Files {
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

// UnresolvedCodeRefs returns the requirement code refs that name neither an
// observed file nor an observed interface.
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

// Conditions is the one place that turns a decision into the three conditions.
// Ingest and the controller both call it, so the two never disagree and never
// write over each other. The kcp-libs condition helpers keep the transition
// time of a condition whose status did not change.
func Conditions(in Input, previous []metav1.Condition) (Decision, []metav1.Condition) {
	decision := Decide(in)
	out := make([]metav1.Condition, 0, 3)
	for _, conditionType := range []string{
		specapi.ConditionSpecValid,
		specapi.ConditionCodeSynced,
		specapi.ConditionDrifted,
	} {
		if before := condition.Of(previous, conditionType); before != nil {
			out = append(out, *before)
		}
	}

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
