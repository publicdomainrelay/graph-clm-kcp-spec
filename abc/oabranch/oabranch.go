package oabranch

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	yaml "sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/mirror"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/yamlx"
)

const (
	Prefix = "open-architecture/"

	ReadmePath = "README.md"

	RepositoryPath = "repository.yaml"

	ArchPath = "arch.yaml"

	SpecsDir = "specs"

	StatusDir = "status"

	ContextDir = "context"

	ChangesDir = "changes"

	ChangesDocPath = "CHANGES.md"

	GitAttributesPath = ".gitattributes"

	GraphDir = "graph"

	GraphVerticesPath = "graph/vertices.jsonl"

	GraphEdgesPath = "graph/edges.jsonl"

	ArchAPIVersion = "open-architecture.dffml.github.io/v0alpha1"

	ArchKind = "OpenArchitecture"

	GeneratedArchKind = "GeneratedArchitecture"

	CodeCommitTrailer = "Code-Commit"

	SpecChangeTrailer = "Spec-Change"

	OpenArchitectureTrailer = "Open-Architecture"

	AcceptanceTrailer = "Acceptance"

	ConflictTrailer = "Conflict"
)

const BranchSeparator = "--"

func Branch(repository string) string {
	return Prefix + repository
}

func Ref(repository string) string {
	return "refs/heads/" + Branch(repository)
}

func BranchFor(repository, codeBranch, defaultBranch string) string {
	if codeBranch == "" || codeBranch == defaultBranch {
		return Branch(repository)
	}
	return Branch(repository) + BranchSeparator + branchSuffix(codeBranch)
}

func RefFor(repository, codeBranch, defaultBranch string) string {
	return "refs/heads/" + BranchFor(repository, codeBranch, defaultBranch)
}

func branchSuffix(codeBranch string) string {
	return Slug(codeBranch)
}

func Slug(codeBranch string) string {
	builder := strings.Builder{}
	for _, char := range codeBranch {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9', char == '-', char == '_', char == '.':
			builder.WriteRune(char)
		default:
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), ".-")
}

func SpecPath(context string) string {
	return SpecsDir + "/" + context + ".yaml"
}

func StatusPath(context string) string {
	return StatusDir + "/" + context + ".yaml"
}

func ContextPath(context string) string {
	return ContextDir + "/" + context + ".md"
}

func ChangePath(change string) string {
	return ChangesDir + "/" + change + ".yaml"
}

type Snapshot struct {
	Repository spec.Repository

	Contexts []spec.SystemContext

	Changes []spec.SpecChange

	ManagedBudget int

	// Branch is the architecture branch these files are written to. Empty means
	// the default branch, Branch(Repository.Name).
	Branch string

	// Baseline is the default branch's architecture, set when Branch is a
	// feature branch. It is what CHANGES.md measures the requirement delta
	// against and which changes it reports as this branch's outcome.
	Baseline *Baseline
}

type Baseline struct {
	Contexts []spec.SystemContext

	Changes []spec.SpecChange
}

func (s Snapshot) branch() string {
	if s.Branch != "" {
		return s.Branch
	}
	return Branch(s.Repository.Name)
}

func (s Snapshot) feature() bool {
	return s.branch() != Branch(s.Repository.Name)
}

type objectMeta struct {
	Name string `json:"name"`

	Namespace string `json:"namespace,omitempty"`

	Labels map[string]string `json:"labels,omitempty"`
}

type repositoryStatusDoc struct {
	HeadCommit string `json:"headCommit,omitempty"`

	IndexedCommit string `json:"indexedCommit,omitempty"`

	ObservedCommit string `json:"observedCommit,omitempty"`

	SyncedCommit string `json:"syncedCommit,omitempty"`

	Phase string `json:"phase,omitempty"`

	Contexts *spec.PopulateCounts `json:"contexts,omitempty"`
}

type repositoryDoc struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata objectMeta `json:"metadata"`

	Spec spec.RepositorySpec `json:"spec"`

	Status repositoryStatusDoc `json:"status"`
}

type statusDoc struct {
	ObservedCommit string `json:"observedCommit,omitempty"`

	SyncedCommit string `json:"syncedCommit,omitempty"`

	SyncedFingerprint string `json:"syncedFingerprint,omitempty"`

	RealizedSpecHash string `json:"realizedSpecHash,omitempty"`

	Conditions []conditionDoc `json:"conditions,omitempty"`

	Observed spec.ObservedFacts `json:"observed,omitempty"`
}

type conditionDoc struct {
	Type string `json:"type"`

	Status string `json:"status"`

	Reason string `json:"reason,omitempty"`

	Message string `json:"message,omitempty"`
}

type changeDoc struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata objectMeta `json:"metadata"`

	Spec changeSpecDoc `json:"spec"`

	Status changeStatusDoc `json:"status"`

	Superseded []supersededDoc `json:"superseded,omitempty"`
}

// changeStatusDoc is the status a change record carries: the outcome, the
// agent's own report with the verify summary, and a progress summary rather
// than one entry per tool call. The full progress list and the raw logs stay
// in kcp and the state dir.
type changeStatusDoc struct {
	Phase string `json:"phase,omitempty"`

	Branch string `json:"branch,omitempty"`

	Commit string `json:"commit,omitempty"`

	VerifyExitCode int `json:"verifyExitCode,omitempty"`

	FilesTouched []string `json:"filesTouched,omitempty"`

	AgentLog string `json:"agentLog,omitempty"`

	Message string `json:"message,omitempty"`

	Acceptance []spec.AcceptanceResult `json:"acceptance,omitempty"`

	RequirementCoverage []spec.RequirementVerdict `json:"requirementCoverage,omitempty"`

	Progress *changeProgressDoc `json:"progress,omitempty"`
}

type changeProgressDoc struct {
	Turns int `json:"turns,omitempty"`

	Tools []changeToolCount `json:"tools,omitempty"`

	Files []string `json:"files,omitempty"`

	Notes int `json:"notes,omitempty"`

	First string `json:"first,omitempty"`

	Last string `json:"last,omitempty"`
}

type changeToolCount struct {
	Tool string `json:"tool"`

	Count int `json:"count"`
}

func changeStatusDocOf(status spec.SpecChangeStatus) changeStatusDoc {
	return changeStatusDoc{
		Phase:               status.Phase,
		Branch:              status.Branch,
		Commit:              status.Commit,
		VerifyExitCode:      status.VerifyExitCode,
		FilesTouched:        status.FilesTouched,
		AgentLog:            status.AgentLog,
		Message:             status.Message,
		Acceptance:          status.Acceptance,
		RequirementCoverage: status.RequirementCoverage,
		Progress:            progressSummary(status.Progress),
	}
}

func (s changeStatusDoc) specChangeStatus() spec.SpecChangeStatus {
	return spec.SpecChangeStatus{
		Phase:               s.Phase,
		Branch:              s.Branch,
		Commit:              s.Commit,
		VerifyExitCode:      s.VerifyExitCode,
		FilesTouched:        s.FilesTouched,
		AgentLog:            s.AgentLog,
		Message:             s.Message,
		Acceptance:          s.Acceptance,
		RequirementCoverage: s.RequirementCoverage,
	}
}

func progressSummary(records []spec.ProgressRecord) *changeProgressDoc {
	if len(records) == 0 {
		return nil
	}
	doc := &changeProgressDoc{}
	tools := map[string]int{}
	files := map[string]bool{}
	for _, record := range records {
		if record.Turn > doc.Turns {
			doc.Turns = record.Turn
		}
		if record.Tool != "" {
			tools[record.Tool]++
		}
		if record.Note != "" {
			doc.Notes++
		}
		for _, file := range record.Files {
			files[file] = true
		}
		if record.At == "" {
			continue
		}
		if doc.First == "" || record.At < doc.First {
			doc.First = record.At
		}
		if record.At > doc.Last {
			doc.Last = record.At
		}
	}
	for _, tool := range sortedKeys(tools) {
		doc.Tools = append(doc.Tools, changeToolCount{Tool: tool, Count: tools[tool]})
	}
	for file := range files {
		doc.Files = append(doc.Files, file)
	}
	sort.Strings(doc.Files)
	return doc
}

func sortedKeys[V any](in map[string]V) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// changeSpecDoc is the compact spec a change record carries: what was attempted
// and how it ended, and a delta summary rather than the whole delta.
// CHANGES.md carries the readable delta.
type changeSpecDoc struct {
	SystemContext string `json:"systemContext,omitempty"`

	Direction string `json:"direction,omitempty"`

	FromSpecHash string `json:"fromSpecHash,omitempty"`
	ToSpecHash   string `json:"toSpecHash,omitempty"`

	FromCommit string `json:"fromCommit,omitempty"`
	ToCommit   string `json:"toCommit,omitempty"`

	Delta *changeDeltaDoc `json:"delta,omitempty"`
}

type changeDeltaDoc struct {
	Counts changeDeltaCounts `json:"counts"`

	Requirements []string `json:"requirements,omitempty"`

	Interfaces []string `json:"interfaces,omitempty"`
}

type changeDeltaCounts struct {
	Added int `json:"added,omitempty"`

	Removed int `json:"removed,omitempty"`

	Changed int `json:"changed,omitempty"`
}

// UnmarshalJSON keeps a record written before the summary - whose delta is the
// full spec.Delta - loading; the summary is not needed to read a branch back.
func (d *changeDeltaDoc) UnmarshalJSON(data []byte) error {
	summary := struct {
		Counts       changeDeltaCounts `json:"counts"`
		Requirements []string          `json:"requirements"`
		Interfaces   []string          `json:"interfaces"`
	}{}
	if err := json.Unmarshal(data, &summary); err != nil {
		*d = changeDeltaDoc{}
		return nil
	}
	d.Counts = summary.Counts
	d.Requirements = summary.Requirements
	d.Interfaces = summary.Interfaces
	return nil
}

func changeSpecDocOf(in spec.SpecChangeSpec) changeSpecDoc {
	return changeSpecDoc{
		SystemContext: in.SystemContext,
		Direction:     in.Direction,
		FromSpecHash:  in.FromSpecHash,
		ToSpecHash:    in.ToSpecHash,
		FromCommit:    in.FromCommit,
		ToCommit:      in.ToCommit,
		Delta:         changeDeltaOf(in.Delta),
	}
}

func (d changeSpecDoc) specChangeSpec() spec.SpecChangeSpec {
	return spec.SpecChangeSpec{
		SystemContext: d.SystemContext,
		Direction:     d.Direction,
		FromSpecHash:  d.FromSpecHash,
		ToSpecHash:    d.ToSpecHash,
		FromCommit:    d.FromCommit,
		ToCommit:      d.ToCommit,
	}
}

func changeDeltaOf(in *spec.Delta) *changeDeltaDoc {
	if in == nil {
		return nil
	}
	counts := in.Count()
	out := &changeDeltaDoc{Counts: changeDeltaCounts{Added: counts.Added, Removed: counts.Removed, Changed: counts.Changed}}
	for _, requirement := range in.Requirements {
		out.Requirements = append(out.Requirements, op(requirement.Op)+requirement.ID)
	}
	for _, declared := range in.Interfaces {
		out.Interfaces = append(out.Interfaces, op(declared.Op)+declared.Name)
	}
	sort.Strings(out.Requirements)
	sort.Strings(out.Interfaces)
	if out.Counts == (changeDeltaCounts{}) && len(out.Requirements) == 0 && len(out.Interfaces) == 0 {
		return nil
	}
	return out
}

type supersededDoc struct {
	Name string `json:"name"`

	Phase string `json:"phase,omitempty"`

	Commit string `json:"commit,omitempty"`

	Message string `json:"message,omitempty"`
}

// archContext is the structure of one context in arch.yaml, not its text: the
// prose, the requirement text and the interfaces live in the spec file it
// names, so an edit to a requirement's wording does not rewrite arch.yaml.
type archContext struct {
	ID string `json:"id"`

	Name string `json:"name"`

	Spec string `json:"spec"`

	Upstream string `json:"upstream,omitempty"`

	Overlay []string `json:"overlay,omitempty"`

	Orchestrator string `json:"orchestrator,omitempty"`

	DependsOn []string `json:"depends_on,omitempty"`

	Introduces []string `json:"introduces,omitempty"`

	Arch *archNodeDoc `json:"arch,omitempty"`

	Requirements []archRequirement `json:"requirements,omitempty"`

	CodeRefIndex []string `json:"codeRefIndex,omitempty"`
}

// archNodeDoc is where a context sits in the architecture document it was
// seeded from; the node body stays in the spec file.
type archNodeDoc struct {
	ID string `json:"id"`

	Kind string `json:"kind,omitempty"`

	Section string `json:"section,omitempty"`

	Form string `json:"form,omitempty"`

	Position int `json:"position,omitempty"`

	Parent string `json:"parent,omitempty"`

	Slot string `json:"slot,omitempty"`
}

type archRequirement struct {
	ID string `json:"id"`

	Level spec.Level `json:"level,omitempty"`
}

type archMetadata struct {
	Name string `json:"name"`

	Source string `json:"source,omitempty"`

	Branch string `json:"branch"`
}

type archDoc struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata archMetadata `json:"metadata"`

	SystemContexts []archContext `json:"system_contexts"`
}

func Files(snapshot Snapshot) (map[string][]byte, error) {
	files := map[string][]byte{}
	name := snapshot.Repository.Name
	if name == "" {
		return nil, fmt.Errorf("oabranch: the snapshot has no repository name")
	}
	files[ReadmePath] = []byte(readme(name))
	files[GitAttributesPath] = []byte(gitAttributes())

	contexts := sortedContexts(snapshot.Contexts)
	repository, err := yamlx.Marshal(repositoryDocOf(snapshot.Repository, contexts))
	if err != nil {
		return nil, fmt.Errorf("oabranch: render %s: %w", RepositoryPath, err)
	}
	files[RepositoryPath] = repository

	refs := graph.CodeRefMap(contexts)
	arch, err := yamlx.Marshal(archDocOf(snapshot.Repository, contexts, snapshot.branch(), refs))
	if err != nil {
		return nil, fmt.Errorf("oabranch: render %s: %w", ArchPath, err)
	}
	files[ArchPath] = arch

	for _, context := range contexts {
		specFile, err := mirror.RenderWithRefs(context.Name, context.Namespace, context.Spec, codeRefIndex(context, refs))
		if err != nil {
			return nil, err
		}
		files[SpecPath(context.Name)] = specFile

		status, err := yamlx.Marshal(statusDocOf(context.Status, commonCommits(contexts)))
		if err != nil {
			return nil, fmt.Errorf("oabranch: render status of %s: %w", context.Name, err)
		}
		files[StatusPath(context.Name)] = status

		document, err := ContextDocument(context, snapshot.ManagedBudget)
		if err != nil {
			return nil, err
		}
		files[ContextPath(context.Name)] = []byte(document)
	}

	if snapshot.feature() {
		files[ChangesDocPath] = []byte(changesDocument(snapshot))
	}

	for _, episode := range episodes(snapshot.Changes) {
		surviving := episode[0]
		data, err := yamlx.Marshal(changeDoc{
			APIVersion: specapi.Group + "/" + specapi.Version,
			Kind:       specapi.SpecChangeKind,
			Metadata:   objectMeta{Name: surviving.Name, Namespace: surviving.Namespace},
			Spec:       changeSpecDocOf(surviving.Spec),
			Status:     changeStatusDocOf(surviving.Status),
			Superseded: superseded(episode),
		})
		if err != nil {
			return nil, fmt.Errorf("oabranch: render change %s: %w", surviving.Name, err)
		}
		files[ChangePath(surviving.Name)] = data
	}

	vertices, edges := graph.Build(graph.Snapshot{Repository: portable(snapshot.Repository), Contexts: contexts, CodeRefs: refs})
	vertexLines, err := vertexJSONL(vertices)
	if err != nil {
		return nil, err
	}
	edgeLines, err := edgeJSONL(edges)
	if err != nil {
		return nil, err
	}
	files[GraphVerticesPath] = vertexLines
	files[GraphEdgesPath] = edgeLines
	return files, nil
}

func ContextDocument(context spec.SystemContext, budget int) (string, error) {
	prose, err := clm.ProseZone(context.Name, context.Spec.Repository, context.Spec.Intent)
	if err != nil {
		return "", fmt.Errorf("oabranch: render context %s: %w", context.Name, err)
	}
	return clm.Document(prose, agent.ContextRefs(context.Spec, context.Status.Observed), budget), nil
}

func codeRefIndex(context spec.SystemContext, refs map[string]graph.CodeRef) []string {
	return graph.ContextRefLines(context, refs)
}

// gitAttributes marks every derived file as generated, so a review of a
// branch or a pull request collapses them and leaves specs/ and CHANGES.md as
// the diff a person reads.
func gitAttributes() string {
	return strings.Join([]string{
		"# Written by specd from kcp; review specs/ and CHANGES.md instead.",
		ArchPath + " linguist-generated=true",
		RepositoryPath + " linguist-generated=true",
		ChangesDir + "/* linguist-generated=true",
		ContextDir + "/* linguist-generated=true",
		GraphDir + "/* linguist-generated=true",
		StatusDir + "/* linguist-generated=true",
		"",
	}, "\n")
}

func readme(repository string) string {
	return "# " + Branch(repository) + "\n\n" +
		"The architecture and specs of `" + repository + "`, as kcp holds them.\n\n" +
		"Written by specd, one commit per change in kcp. An orphan branch: it shares no\n" +
		"history with the code and is never checked out next to it, so an agent working\n" +
		"on the code never mistakes the spec for the code.\n\n" +
		"A `.gitattributes` marks every derived file `linguist-generated`, so a\n" +
		"review shows `specs/` and `CHANGES.md` and collapses the rest.\n\n" +
		"| path | holds |\n" +
		"| --- | --- |\n" +
		"| `arch.yaml` | the structure of the repository as generated system contexts (`kind: GeneratedArchitecture`): each one's id, upstream, depends_on, arch node, requirement ids and levels, and a pointer to its spec file |\n" +
		"| `repository.yaml` | the Repository manifest, its populate state, and the commits every context shares |\n" +
		"| `specs/<context>.yaml` | each context's declared spec; edit here to change kcp |\n" +
		"| `status/<context>.yaml` | observed code facts and conditions |\n" +
		"| `context/<context>.md` | the context's prose and resolved code references; the spec lives in `specs/` |\n" +
		"| `changes/<name>.yaml` | each SpecChange: direction, a delta summary (counts and ids), a progress summary, the agent's report and the verify summary |\n" +
		"| `CHANGES.md` | on a feature branch: the requirement delta against the default branch |\n" +
		"| `graph/*.jsonl` | the context graph, one vertex or edge per line |\n"
}

type commonCommitsDoc struct {
	ObservedCommit string

	SyncedCommit string
}

// commonCommits is the observed and synced commit every context shares, when
// one value holds for all of them. repository.yaml carries it once, and a
// status file repeats it only where its context differs.
func commonCommits(contexts []spec.SystemContext) commonCommitsDoc {
	common := commonCommitsDoc{}
	if len(contexts) == 0 {
		return common
	}
	common = commonCommitsDoc{
		ObservedCommit: contexts[0].Status.ObservedCommit,
		SyncedCommit:   contexts[0].Status.SyncedCommit,
	}
	for _, context := range contexts[1:] {
		if context.Status.ObservedCommit != common.ObservedCommit {
			common.ObservedCommit = ""
		}
		if context.Status.SyncedCommit != common.SyncedCommit {
			common.SyncedCommit = ""
		}
	}
	return common
}

func repositoryDocOf(repository spec.Repository, contexts []spec.SystemContext) repositoryDoc {
	repository = portable(repository)
	common := commonCommits(contexts)
	return repositoryDoc{
		APIVersion: specapi.Group + "/" + specapi.Version,
		Kind:       specapi.RepositoryKind,
		Metadata:   objectMeta{Name: repository.Name, Namespace: repository.Namespace, Labels: repository.Labels},
		Spec:       repository.Spec,
		Status: repositoryStatusDoc{
			HeadCommit:     repository.Status.HeadCommit,
			IndexedCommit:  repository.Status.IndexedCommit,
			ObservedCommit: common.ObservedCommit,
			SyncedCommit:   common.SyncedCommit,
			Phase:          repository.Status.Phase,
			Contexts:       repository.Status.Contexts,
		},
	}
}

func statusDocOf(status spec.SystemContextStatus, common commonCommitsDoc) statusDoc {
	conditions := make([]conditionDoc, 0, len(status.Conditions))
	for _, condition := range status.Conditions {
		conditions = append(conditions, conditionDoc{
			Type:    condition.Type,
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}
	sort.Slice(conditions, func(left, right int) bool { return conditions[left].Type < conditions[right].Type })
	doc := statusDoc{
		SyncedFingerprint: status.SyncedFingerprint,
		RealizedSpecHash:  status.RealizedSpecHash,
		Conditions:        conditions,
		Observed:          status.Observed,
	}
	if status.ObservedCommit != common.ObservedCommit {
		doc.ObservedCommit = status.ObservedCommit
	}
	if status.SyncedCommit != common.SyncedCommit {
		doc.SyncedCommit = status.SyncedCommit
	}
	return doc
}

func archDocOf(repository spec.Repository, contexts []spec.SystemContext, branch string, refs map[string]graph.CodeRef) archDoc {
	doc := archDoc{
		APIVersion: ArchAPIVersion,
		Kind:       GeneratedArchKind,
		Metadata:   archMetadata{Name: repository.Name, Source: sourceOf(repository), Branch: branch},
	}
	for _, context := range contexts {
		if context.Spec.Arch != nil && context.Spec.Arch.Kind == spec.ArchKindDocument {
			continue
		}
		declared := spec.Canonicalize(context.Spec)
		id := "sc." + context.Name
		if declared.Arch != nil && declared.Arch.ID != "" {
			id = declared.Arch.ID
		}
		doc.SystemContexts = append(doc.SystemContexts, archContext{
			ID:           id,
			Name:         context.Name,
			Spec:         SpecPath(context.Name),
			Upstream:     declared.Upstream,
			Overlay:      declared.Overlay,
			Orchestrator: declared.Orchestrator,
			DependsOn:    declared.DependsOn,
			Introduces:   declared.Introduces,
			Arch:         archNodeOf(declared.Arch),
			Requirements: archRequirements(declared.Requirements),
			CodeRefIndex: codeRefIndex(context, refs),
		})
	}
	if doc.SystemContexts == nil {
		doc.SystemContexts = []archContext{}
	}
	return doc
}

func archNodeOf(arch *spec.ArchSpec) *archNodeDoc {
	if arch == nil || arch.ID == "" {
		return nil
	}
	return &archNodeDoc{
		ID:       arch.ID,
		Kind:     arch.Kind,
		Section:  arch.Section,
		Form:     arch.Form,
		Position: arch.Position,
		Parent:   arch.Parent,
		Slot:     arch.Slot,
	}
}

func archRequirements(requirements []spec.Requirement) []archRequirement {
	if len(requirements) == 0 {
		return nil
	}
	out := make([]archRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		out = append(out, archRequirement{ID: requirement.ID, Level: requirement.Level})
	}
	return out
}

func sourceOf(repository spec.Repository) string {
	source := repository.Spec.Source
	if source != nil && source.Git != nil {
		if source.Git.Ref != "" {
			return source.Git.URL + "@" + source.Git.Ref
		}
		return source.Git.URL
	}
	return ""
}

func portable(repository spec.Repository) spec.Repository {
	out := repository
	out.Status.ResolvedPath = ""
	out.Spec.Path = ""
	if out.Spec.Source != nil && out.Spec.Source.Path != "" {
		source := *out.Spec.Source
		source.Path = ""
		out.Spec.Source = &source
	}
	return out
}

func sortedContexts(contexts []spec.SystemContext) []spec.SystemContext {
	out := append([]spec.SystemContext{}, contexts...)
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out
}

func vertexJSONL(sets []graph.VertexSet) ([]byte, error) {
	lines := []string{}
	for _, set := range sets {
		for _, vertex := range set.Rows {
			row := vertex.Row()
			row["label"] = set.Label
			data, err := json.Marshal(row)
			if err != nil {
				return nil, fmt.Errorf("oabranch: render vertex %s %d: %w", set.Label, vertex.ID, err)
			}
			lines = append(lines, string(data))
		}
	}
	return joinSorted(lines), nil
}

func edgeJSONL(sets []graph.EdgeSet) ([]byte, error) {
	lines := []string{}
	for _, set := range sets {
		for _, edge := range set.Rows {
			data, err := json.Marshal(map[string]any{"type": set.Type, "from": edge.From, "to": edge.To, "fromLabel": set.FromLabel, "toLabel": set.ToLabel})
			if err != nil {
				return nil, fmt.Errorf("oabranch: render edge %s: %w", set.Type, err)
			}
			lines = append(lines, string(data))
		}
	}
	return joinSorted(lines), nil
}

func joinSorted(lines []string) []byte {
	sort.Strings(lines)
	unique := lines[:0]
	for index, line := range lines {
		if index > 0 && line == lines[index-1] {
			continue
		}
		unique = append(unique, line)
	}
	if len(unique) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(unique, "\n") + "\n")
}

func BlobID(data []byte) string {
	hasher := sha1.New()
	fmt.Fprintf(hasher, "blob %d\x00", len(data))
	hasher.Write(data)
	return hex.EncodeToString(hasher.Sum(nil))
}

type Plan struct {
	Write map[string][]byte

	Remove []string

	Added []string

	Modified []string
}

func (p Plan) Empty() bool {
	return len(p.Write) == 0 && len(p.Remove) == 0
}

func (p Plan) Paths() []string {
	paths := append(append(append([]string{}, p.Added...), p.Modified...), p.Remove...)
	sort.Strings(paths)
	return paths
}

func PlanCommit(tip map[string]string, files map[string][]byte) Plan {
	plan := Plan{Write: map[string][]byte{}}
	for path, data := range files {
		blob, ok := tip[path]
		switch {
		case !ok:
			plan.Write[path] = data
			plan.Added = append(plan.Added, path)
		case blob != BlobID(data):
			plan.Write[path] = data
			plan.Modified = append(plan.Modified, path)
		}
	}
	for path := range tip {
		if _, ok := files[path]; !ok {
			plan.Remove = append(plan.Remove, path)
		}
	}
	sort.Strings(plan.Added)
	sort.Strings(plan.Modified)
	sort.Strings(plan.Remove)
	return plan
}

type ObjectRef struct {
	Kind string

	Name string

	Generation int64

	ResourceVersion string

	Origin string
}

func Message(repository string, plan Plan, snapshot Snapshot, previous map[string][]byte, objects []ObjectRef, codeCommits []string, conflicts ...string) string {
	builder := strings.Builder{}
	subjects := Subjects(plan, snapshot, previous)
	if len(subjects) == 0 {
		subjects = []string{fmt.Sprintf("architecture(%s): %s", repository, summary(plan))}
	}
	builder.WriteString(subjects[0] + "\n\n")
	if len(subjects) > 1 {
		builder.WriteString(strings.Join(subjects[1:], "\n") + "\n\n")
	}
	for _, path := range plan.Added {
		fmt.Fprintf(&builder, "A %s\n", path)
	}
	for _, path := range plan.Modified {
		fmt.Fprintf(&builder, "M %s\n", path)
	}
	for _, path := range plan.Remove {
		fmt.Fprintf(&builder, "D %s\n", path)
	}
	sorted := append([]ObjectRef{}, objects...)
	sort.Slice(sorted, func(left, right int) bool {
		if sorted[left].Kind != sorted[right].Kind {
			return sorted[left].Kind < sorted[right].Kind
		}
		return sorted[left].Name < sorted[right].Name
	})
	if len(sorted) > 0 {
		builder.WriteString("\n")
	}
	for _, object := range sorted {
		fmt.Fprintf(&builder, "%s %s generation=%d resourceVersion=%s", object.Kind, object.Name, object.Generation, object.ResourceVersion)
		if object.Origin != "" {
			fmt.Fprintf(&builder, " origin=%s", object.Origin)
		}
		builder.WriteString("\n")
	}
	specChanges := specChangeTrailers(plan, snapshot)
	if len(specChanges) > 0 {
		builder.WriteString("\n")
	}
	for _, name := range specChanges {
		fmt.Fprintf(&builder, "%s: %s\n", SpecChangeTrailer, name)
	}
	commits := append([]string{}, codeCommits...)
	sort.Strings(commits)
	if len(commits) > 0 {
		builder.WriteString("\n")
	}
	seen := ""
	for _, commit := range commits {
		if commit == "" || commit == seen {
			continue
		}
		seen = commit
		fmt.Fprintf(&builder, "%s: %s\n", CodeCommitTrailer, commit)
	}
	sortedConflicts := append([]string{}, conflicts...)
	sort.Strings(sortedConflicts)
	if len(sortedConflicts) > 0 && len(commits) == 0 {
		builder.WriteString("\n")
	}
	for _, conflict := range sortedConflicts {
		fmt.Fprintf(&builder, "%s: %s\n", ConflictTrailer, conflict)
	}
	return builder.String()
}

// specChangeTrailers names the realized changes behind the spec files this
// commit carries, newest per context. A commit that touches no spec file gets
// no Spec-Change trailer.
func specChangeTrailers(plan Plan, snapshot Snapshot) []string {
	newest := map[string]spec.SpecChange{}
	touched := plan.touched()
	for _, context := range snapshot.Contexts {
		if !touched[SpecPath(context.Name)] {
			continue
		}
		for _, change := range snapshot.Changes {
			if change.Spec.SystemContext != context.Name || change.Status.Phase != specapi.PhaseSucceeded {
				continue
			}
			if current, ok := newest[context.Name]; !ok || change.CreationTimestamp.After(current.CreationTimestamp.Time) {
				newest[context.Name] = change
			}
		}
	}
	names := make([]string, 0, len(newest))
	for _, change := range newest {
		names = append(names, change.Name)
	}
	sort.Strings(names)
	return names
}

func summary(plan Plan) string {
	parts := []string{}
	for _, part := range []struct {
		count int
		word  string
	}{{len(plan.Added), "added"}, {len(plan.Modified), "modified"}, {len(plan.Remove), "removed"}} {
		if part.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", part.count, part.word))
		}
	}
	if len(parts) == 0 {
		return "no change"
	}
	return strings.Join(parts, ", ")
}

func TouchedObjects(plan Plan, snapshot Snapshot, origin func(kind, name string) string) ([]ObjectRef, []string) {
	touched := map[string]bool{}
	for _, path := range plan.Paths() {
		touched[path] = true
	}
	refs := []ObjectRef{}
	commits := []string{}
	if touched[RepositoryPath] {
		refs = append(refs, ObjectRef{
			Kind:            specapi.RepositoryKind,
			Name:            snapshot.Repository.Name,
			Generation:      snapshot.Repository.Generation,
			ResourceVersion: snapshot.Repository.ResourceVersion,
			Origin:          origin(specapi.RepositoryKind, snapshot.Repository.Name),
		})
	}
	for _, context := range snapshot.Contexts {
		if touched[SpecPath(context.Name)] || touched[StatusPath(context.Name)] || touched[ContextPath(context.Name)] {
			refs = append(refs, ObjectRef{
				Kind:            specapi.SystemContextKind,
				Name:            context.Name,
				Generation:      context.Generation,
				ResourceVersion: context.ResourceVersion,
				Origin:          origin(specapi.SystemContextKind, context.Name),
			})
		}
	}
	for _, change := range snapshot.Changes {
		if !touched[ChangePath(change.Name)] {
			continue
		}
		refs = append(refs, ObjectRef{
			Kind:            specapi.SpecChangeKind,
			Name:            change.Name,
			Generation:      change.Generation,
			ResourceVersion: change.ResourceVersion,
		})
		if change.Status.Phase == specapi.PhaseSucceeded && change.Status.Commit != "" {
			commits = append(commits, change.Status.Commit)
		}
	}
	return refs, commits
}

// IsChangePath reports whether a branch path is a SpecChange record.
func IsChangePath(path string) bool {
	name, ok := strings.CutPrefix(path, ChangesDir+"/")
	return ok && name != "" && !strings.Contains(name, "/") && strings.HasSuffix(name, ".yaml")
}

// IsSpecToCodeChangePath names a change the spec-to-code direction raised: a
// spec edit a person or a model made, which is where a feature branch's own
// declared delta begins.
func IsSpecToCodeChangePath(path string) bool {
	name, ok := strings.CutPrefix(path, ChangesDir+"/")
	return ok && IsChangePath(path) && strings.Contains(name, "-s2c-")
}

// ChangePaths lists the change records a branch tree holds, from a path to blob
// map such as oagit.Store.Blobs returns.
func ChangePaths(blobs map[string]string) []string {
	paths := make([]string, 0, len(blobs))
	for path := range blobs {
		if IsChangePath(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

// ChangeFiles parses the SpecChange records a branch tree holds.
func ChangeFiles(files map[string][]byte) ([]spec.SpecChange, error) {
	names := make([]string, 0, len(files))
	for path := range files {
		if IsChangePath(path) {
			names = append(names, path)
		}
	}
	sort.Strings(names)
	out := make([]spec.SpecChange, 0, len(names))
	for _, path := range names {
		doc := changeDoc{}
		if err := yaml.Unmarshal(files[path], &doc); err != nil {
			return nil, fmt.Errorf("oabranch: parse %s: %w", path, err)
		}
		change := spec.SpecChange{}
		change.APIVersion = doc.APIVersion
		change.Kind = doc.Kind
		change.Name = doc.Metadata.Name
		change.Namespace = doc.Metadata.Namespace
		change.Spec = doc.Spec.specChangeSpec()
		change.Status = doc.Status.specChangeStatus()
		out = append(out, change)
	}
	return out, nil
}

func SpecFiles(files map[string][]byte) (map[string]spec.SystemContext, error) {
	out := map[string]spec.SystemContext{}
	for path, data := range files {
		name, ok := strings.CutPrefix(path, SpecsDir+"/")
		if !ok || !strings.HasSuffix(name, ".yaml") || strings.Contains(name, "/") {
			continue
		}
		name = strings.TrimSuffix(name, ".yaml")
		context, err := mirror.Parse(name, data)
		if err != nil {
			return nil, err
		}
		out[name] = context
	}
	return out, nil
}

func RepositoryFile(files map[string][]byte) (spec.Repository, error) {
	data, ok := files[RepositoryPath]
	if !ok {
		return spec.Repository{}, fmt.Errorf("oabranch: the branch has no %s", RepositoryPath)
	}
	doc := repositoryDoc{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return spec.Repository{}, fmt.Errorf("oabranch: parse %s: %w", RepositoryPath, err)
	}
	if doc.Kind != specapi.RepositoryKind {
		return spec.Repository{}, fmt.Errorf("oabranch: %s is a %q, want %s", RepositoryPath, doc.Kind, specapi.RepositoryKind)
	}
	repository := spec.Repository{}
	repository.APIVersion = doc.APIVersion
	repository.Kind = doc.Kind
	repository.Name = doc.Metadata.Name
	repository.Namespace = doc.Metadata.Namespace
	repository.Labels = doc.Metadata.Labels
	repository.Spec = doc.Spec
	return repository, nil
}
