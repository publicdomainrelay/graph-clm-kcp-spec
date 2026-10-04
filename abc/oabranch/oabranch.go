package oabranch

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/mirror"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
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

	GraphVerticesPath = "graph/vertices.jsonl"

	GraphEdgesPath = "graph/edges.jsonl"

	ArchAPIVersion = "open-architecture.dffml.github.io/v0alpha1"

	ArchKind = "OpenArchitecture"

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
}

type objectMeta struct {
	Name string `json:"name"`

	Namespace string `json:"namespace,omitempty"`

	Labels map[string]string `json:"labels,omitempty"`
}

type repositoryStatusDoc struct {
	HeadCommit string `json:"headCommit,omitempty"`

	IndexedCommit string `json:"indexedCommit,omitempty"`

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

	Spec spec.SpecChangeSpec `json:"spec"`

	Status spec.SpecChangeStatus `json:"status"`
}

type archContext struct {
	ID string `json:"id"`

	Name string `json:"name"`

	Upstream string `json:"upstream,omitempty"`

	Overlay []string `json:"overlay,omitempty"`

	Orchestrator string `json:"orchestrator,omitempty"`

	DependsOn []string `json:"depends_on,omitempty"`

	Introduces []string `json:"introduces,omitempty"`

	Intent string `json:"intent,omitempty"`

	Requirements []spec.Requirement `json:"requirements,omitempty"`

	Interfaces []spec.Interface `json:"interfaces,omitempty"`

	Code []string `json:"code,omitempty"`
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

	repository, err := yaml.Marshal(repositoryDocOf(snapshot.Repository))
	if err != nil {
		return nil, fmt.Errorf("oabranch: render %s: %w", RepositoryPath, err)
	}
	files[RepositoryPath] = repository

	contexts := sortedContexts(snapshot.Contexts)
	arch, err := yaml.Marshal(archDocOf(snapshot.Repository, contexts))
	if err != nil {
		return nil, fmt.Errorf("oabranch: render %s: %w", ArchPath, err)
	}
	files[ArchPath] = arch

	for _, context := range contexts {
		specFile, err := mirror.Render(context.Name, context.Namespace, context.Spec)
		if err != nil {
			return nil, err
		}
		files[SpecPath(context.Name)] = specFile

		status, err := yaml.Marshal(statusDocOf(context.Status))
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

	for _, change := range snapshot.Changes {
		data, err := yaml.Marshal(changeDoc{
			APIVersion: specapi.Group + "/" + specapi.Version,
			Kind:       specapi.SpecChangeKind,
			Metadata:   objectMeta{Name: change.Name, Namespace: change.Namespace},
			Spec:       change.Spec,
			Status:     change.Status,
		})
		if err != nil {
			return nil, fmt.Errorf("oabranch: render change %s: %w", change.Name, err)
		}
		files[ChangePath(change.Name)] = data
	}

	vertices, edges := graph.Build(graph.Snapshot{Repository: portable(snapshot.Repository), Contexts: contexts})
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
	modelZone, err := clm.RenderModelZone(context.Name, context.Spec.Repository, context.Spec)
	if err != nil {
		return "", fmt.Errorf("oabranch: render context %s: %w", context.Name, err)
	}
	return clm.Document(modelZone, agent.ContextRefs(context.Spec, context.Status.Observed), budget), nil
}

func readme(repository string) string {
	return "# " + Branch(repository) + "\n\n" +
		"The architecture and specs of `" + repository + "`, as kcp holds them.\n\n" +
		"Written by specd, one commit per change in kcp. An orphan branch: it shares no\n" +
		"history with the code and is never checked out next to it, so an agent working\n" +
		"on the code never mistakes the spec for the code.\n\n" +
		"| path | holds |\n" +
		"| --- | --- |\n" +
		"| `arch.yaml` | the whole repository as open architecture system contexts |\n" +
		"| `repository.yaml` | the Repository manifest and its populate state |\n" +
		"| `specs/<context>.yaml` | each context's declared spec; edit here to change kcp |\n" +
		"| `status/<context>.yaml` | observed code facts and conditions |\n" +
		"| `context/<context>.md` | the CLM context document a model reads and edits |\n" +
		"| `changes/<name>.yaml` | each SpecChange: direction, delta, progress, outcome |\n" +
		"| `graph/*.jsonl` | the context graph, one vertex or edge per line |\n"
}

func repositoryDocOf(repository spec.Repository) repositoryDoc {
	repository = portable(repository)
	return repositoryDoc{
		APIVersion: specapi.Group + "/" + specapi.Version,
		Kind:       specapi.RepositoryKind,
		Metadata:   objectMeta{Name: repository.Name, Namespace: repository.Namespace, Labels: repository.Labels},
		Spec:       repository.Spec,
		Status: repositoryStatusDoc{
			HeadCommit:    repository.Status.HeadCommit,
			IndexedCommit: repository.Status.IndexedCommit,
			Phase:         repository.Status.Phase,
			Contexts:      repository.Status.Contexts,
		},
	}
}

func statusDocOf(status spec.SystemContextStatus) statusDoc {
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
	return statusDoc{
		ObservedCommit:    status.ObservedCommit,
		SyncedCommit:      status.SyncedCommit,
		SyncedFingerprint: status.SyncedFingerprint,
		RealizedSpecHash:  status.RealizedSpecHash,
		Conditions:        conditions,
		Observed:          status.Observed,
	}
}

func archDocOf(repository spec.Repository, contexts []spec.SystemContext) archDoc {
	doc := archDoc{
		APIVersion: ArchAPIVersion,
		Kind:       ArchKind,
		Metadata:   archMetadata{Name: repository.Name, Source: sourceOf(repository), Branch: Branch(repository.Name)},
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
			Upstream:     declared.Upstream,
			Overlay:      declared.Overlay,
			Orchestrator: declared.Orchestrator,
			DependsOn:    declared.DependsOn,
			Introduces:   declared.Introduces,
			Intent:       declared.Intent,
			Requirements: declared.Requirements,
			Interfaces:   declared.Interfaces,
			Code:         context.Status.Observed.Files,
		})
	}
	if doc.SystemContexts == nil {
		doc.SystemContexts = []archContext{}
	}
	return doc
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

func Message(repository string, plan Plan, objects []ObjectRef, codeCommits []string, conflicts ...string) string {
	builder := strings.Builder{}
	fmt.Fprintf(&builder, "open-architecture: %s: %s\n\n", repository, summary(plan))
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
	commits := append([]string{}, codeCommits...)
	sort.Strings(commits)
	if len(commits) > 0 {
		builder.WriteString("\n")
	}
	previous := ""
	for _, commit := range commits {
		if commit == "" || commit == previous {
			continue
		}
		previous = commit
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
