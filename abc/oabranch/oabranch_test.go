package oabranch

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/mirror"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func calcSnapshot() Snapshot {
	repository := spec.Repository{}
	repository.Name = "calc"
	repository.Namespace = "default"
	repository.Generation = 2
	repository.ResourceVersion = "41"
	repository.Spec.Source = &spec.RepositorySource{Git: &spec.GitSource{URL: "https://example.com/calc.git", Ref: "main"}}
	repository.Status.HeadCommit = "aaaa"
	repository.Status.ResolvedPath = "/home/someone/calc"
	repository.Status.Phase = "Populated"

	context := spec.SystemContext{}
	context.Name = "calc"
	context.Namespace = "default"
	context.Generation = 3
	context.ResourceVersion = "42"
	context.Spec = spec.SystemContextSpec{
		Repository: "calc",
		Upstream:   "self",
		Intent:     "Integer arithmetic.",
		Requirements: []spec.Requirement{
			{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum.", CodeRefs: []string{"function:abc", "file:calc/calc.go"}},
		},
		Interfaces: []spec.Interface{{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go"}},
		CodeRefs:   []string{"file:calc/calc.go"},
	}
	context.Status.Observed = spec.ObservedFacts{
		Files:       []string{"calc/calc.go"},
		Interfaces:  []spec.ObservedInterface{{Name: "Add", Kind: "function", File: "calc/calc.go", Line: 3, CodegraphID: "function:abc"}},
		Fingerprint: "fp1",
	}
	context.Status.Conditions = []metav1.Condition{{Type: "SpecValid", Status: metav1.ConditionTrue, Reason: "Valid", LastTransitionTime: metav1.Now()}}

	change := spec.SpecChange{}
	change.Name = "calc-s2c-12345678"
	change.Namespace = "default"
	change.Spec = spec.SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionSpecToCode}
	change.Status = spec.SpecChangeStatus{Phase: specapi.PhaseSucceeded, Commit: "c0ffee"}

	return Snapshot{Repository: repository, Contexts: []spec.SystemContext{context}, Changes: []spec.SpecChange{change}}
}

func TestFilesLayout(t *testing.T) {
	files, err := Files(calcSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{ReadmePath, GitAttributesPath, RepositoryPath, ArchPath, SpecPath("calc"), StatusPath("calc"), ContextPath("calc"), ChangePath("calc-s2c-12345678"), GraphVerticesPath, GraphEdgesPath} {
		if _, ok := files[path]; !ok {
			t.Errorf("missing %s", path)
		}
	}
	if strings.Contains(string(files[RepositoryPath]), "/home/someone") {
		t.Errorf("repository.yaml carries the machine path:\n%s", files[RepositoryPath])
	}
	if strings.Contains(string(files[StatusPath("calc")]), "lastTransitionTime") {
		t.Errorf("status carries a timestamp:\n%s", files[StatusPath("calc")])
	}
	if strings.Contains(string(files[SpecPath("calc")]), "codeRefs:\n- file:calc/calc.go\n  interfaces") {
		t.Errorf("spec file carries the derived refs")
	}
	arch := string(files[ArchPath])
	for _, want := range []string{"id: sc.calc", "name: calc", "spec: specs/calc.yaml", "id: r.add", "level: MUST"} {
		if !strings.Contains(arch, want) {
			t.Errorf("arch.yaml lacks %q:\n%s", want, arch)
		}
	}
	for _, unwanted := range []string{"Integer arithmetic.", "Add returns the sum.", "signature:"} {
		if strings.Contains(arch, unwanted) {
			t.Errorf("arch.yaml repeats the spec's text %q:\n%s", unwanted, arch)
		}
	}
	if !strings.Contains(string(files[ContextPath("calc")]), "function:abc") {
		t.Errorf("context document lacks the resolved ref:\n%s", files[ContextPath("calc")])
	}
	if !strings.Contains(string(files[GraphVerticesPath]), `"label":"SpecContext"`) {
		t.Errorf("vertices lack the context:\n%s", files[GraphVerticesPath])
	}
}

func TestArchYamlDoesNotRepeatTheSpecText(t *testing.T) {
	before, err := Files(calcSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := calcSnapshot()
	snapshot.Contexts[0].Spec.Intent = "Integer arithmetic, reworded."
	snapshot.Contexts[0].Spec.Requirements[0].Text = "Add returns the sum of two integers."
	snapshot.Contexts[0].Spec.Interfaces[0].Signature = "func Add(a, b int) int // reworded"
	after, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(before[ArchPath]) != string(after[ArchPath]) {
		t.Errorf("arch.yaml moved when only the spec text did:\n%s", after[ArchPath])
	}
	if string(before[SpecPath("calc")]) == string(after[SpecPath("calc")]) {
		t.Error("the spec file did not move when its text did")
	}
}

func TestArchYamlMovesWhenTheStructureDoes(t *testing.T) {
	before, err := Files(calcSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := calcSnapshot()
	snapshot.Contexts[0].Spec.Requirements = append(snapshot.Contexts[0].Spec.Requirements,
		spec.Requirement{ID: "r.sub", Level: spec.LevelShould, Text: "Subtract returns the difference."})
	after, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	arch := string(after[ArchPath])
	if !strings.Contains(arch, "id: r.sub") || !strings.Contains(arch, "level: SHOULD") {
		t.Errorf("arch.yaml lacks the added requirement's id and level:\n%s", arch)
	}
	if strings.Contains(arch, "Subtract returns the difference.") {
		t.Errorf("arch.yaml carries the added requirement's text:\n%s", arch)
	}
	if string(before[ArchPath]) == arch {
		t.Error("arch.yaml did not move when a requirement was added")
	}
}

func TestGitAttributesCollapsesTheDerivedFiles(t *testing.T) {
	files, err := Files(calcSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	attributes, ok := files[GitAttributesPath]
	if !ok {
		t.Fatal("the branch has no .gitattributes")
	}
	for _, marked := range []string{"arch.yaml", "repository.yaml", "changes/*", "context/*", "graph/*", "status/*"} {
		if !strings.Contains(string(attributes), marked+" linguist-generated=true") {
			t.Errorf(".gitattributes does not mark %s:\n%s", marked, attributes)
		}
	}
	for _, line := range strings.Split(string(attributes), "\n") {
		if !strings.Contains(line, "linguist-generated") {
			continue
		}
		if strings.Contains(line, "specs/") || strings.Contains(line, "CHANGES.md") {
			t.Errorf(".gitattributes marks the reviewable diff: %q", line)
		}
	}
}

func TestFilesAreDeterministic(t *testing.T) {
	first, err := Files(calcSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := calcSnapshot()
	snapshot.Contexts[0].Status.Conditions[0].LastTransitionTime = metav1.Unix(0, 0)
	snapshot.Repository.Status.ResolvedPath = "/elsewhere"
	second, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range first {
		if string(second[path]) != string(data) {
			t.Errorf("%s differs between two renders of the same state", path)
		}
	}
}

func TestFilesAreDeterministicWithManyRefs(t *testing.T) {
	snapshot := calcSnapshot()
	context := &snapshot.Contexts[0]
	for index := 0; index < 24; index++ {
		reference := fmt.Sprintf("struct:%02x%02x", index*7, index*13)
		context.Spec.Requirements[0].CodeRefs = append(context.Spec.Requirements[0].CodeRefs, reference)
		context.Status.Observed.Interfaces = append(context.Status.Observed.Interfaces, spec.ObservedInterface{
			Name: fmt.Sprintf("Type%02d", index), Kind: "struct", File: "calc/calc.go",
			Line: index + 1, CodegraphID: reference,
		})
	}
	first, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 10; attempt++ {
		next, err := Files(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		for path, data := range first {
			if string(next[path]) != string(data) {
				t.Fatalf("%s differs between two renders of the same state:\n%s\n---\n%s", path, data, next[path])
			}
		}
	}
}

func TestFilesAreDeterministicWithMapFields(t *testing.T) {
	snapshot := calcSnapshot()
	snapshot.Repository.Labels = map[string]string{
		"app.kubernetes.io/part-of":  "calc",
		"app.kubernetes.io/name":     "calc",
		"helm.sh/chart":              "calc-0.1.0",
		"app.kubernetes.io/instance": "calc-7f3a",
	}
	snapshot.Repository.Spec.Acceptance = []spec.AcceptanceStep{{
		Name:    "unit",
		Command: []string{"go", "test", "./..."},
		Env: map[string]string{
			"HOME": "/root", "PATH": "/usr/bin:/bin", "BOB_WORKSPACE": "bob",
			"GOCACHE": "/tmp/cache", "OPERATOR_NAMESPACE": "default",
		},
	}}
	node := map[string]any{}
	document := map[string]any{}
	for index := 0; index < 20; index++ {
		node[fmt.Sprintf("k:%08x", index*2654435761)] = index
		document[fmt.Sprintf("doc:%02x", index*7)] = fmt.Sprintf("value-%d", index)
	}
	snapshot.Contexts[0].Spec.Arch = &spec.ArchSpec{
		ID:       "deploy.calc",
		Kind:     spec.ArchKindNode,
		Node:     node,
		Document: document,
	}
	first, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		next, err := Files(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		for path, data := range first {
			if string(next[path]) != string(data) {
				t.Fatalf("%s differs between two renders of the same state:\n%s\n---\n%s", path, data, next[path])
			}
		}
	}
}

func TestBlobIDMatchesGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	data := []byte("spec: calc\n")
	command := exec.Command(git, "hash-object", "--stdin")
	command.Stdin = strings.NewReader(string(data))
	out, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := BlobID(data), strings.TrimSpace(string(out)); got != want {
		t.Fatalf("BlobID = %s, git says %s", got, want)
	}
}

func TestPlanCommit(t *testing.T) {
	tip := map[string]string{
		"same.yaml":    BlobID([]byte("a")),
		"changed.yaml": BlobID([]byte("b")),
		"gone.yaml":    BlobID([]byte("c")),
	}
	files := map[string][]byte{
		"same.yaml":    []byte("a"),
		"changed.yaml": []byte("B"),
		"new.yaml":     []byte("d"),
	}
	plan := PlanCommit(tip, files)
	if strings.Join(plan.Added, ",") != "new.yaml" || strings.Join(plan.Modified, ",") != "changed.yaml" || strings.Join(plan.Remove, ",") != "gone.yaml" {
		t.Fatalf("plan = %+v", plan)
	}
	if _, ok := plan.Write["same.yaml"]; ok {
		t.Fatal("an unchanged file is rewritten")
	}
	if !PlanCommit(map[string]string{"x": BlobID([]byte("x"))}, map[string][]byte{"x": []byte("x")}).Empty() {
		t.Fatal("identical state plans a commit")
	}
}

func TestMessageNamesObjectsAndCodeCommits(t *testing.T) {
	snapshot := calcSnapshot()
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanCommit(map[string]string{}, files)
	objects, commits := TouchedObjects(plan, snapshot, func(kind, name string) string {
		if kind == specapi.SystemContextKind {
			return specapi.OriginCLM
		}
		return ""
	})
	message := Message("calc", plan, snapshot, nil, objects, commits)
	for _, want := range []string{
		"spec(calc): +Add +r.add ~intent ~upstream\n",
		"A specs/calc.yaml",
		"SystemContext calc generation=3 resourceVersion=42 origin=clm",
		"SpecChange calc-s2c-12345678",
		"Spec-Change: calc-s2c-12345678",
		"Code-Commit: c0ffee",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("message lacks %q:\n%s", want, message)
		}
	}
}

func TestFilesCarryReadableRefs(t *testing.T) {
	files, err := Files(calcSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	specFile := string(files[SpecPath("calc")])
	if !strings.Contains(specFile, "codeRefIndex:") || !strings.Contains(specFile, "function:abc Add@calc/calc.go:3") {
		t.Errorf("the spec file does not index its refs:\n%s", specFile)
	}
	arch := string(files[ArchPath])
	if !strings.Contains(arch, "kind: GeneratedArchitecture") {
		t.Errorf("arch.yaml is not its own kind:\n%s", arch)
	}
	if !strings.Contains(arch, "branch: open-architecture/calc") {
		t.Errorf("arch.yaml names the wrong branch:\n%s", arch)
	}
	if !strings.Contains(arch, "Add@calc/calc.go:3") {
		t.Errorf("arch.yaml does not index its refs:\n%s", arch)
	}
	edges := string(files[GraphEdgesPath])
	if !strings.Contains(edges, `"type":"REFERENCES"`) {
		t.Errorf("the graph has no REFERENCES edge:\n%s", edges)
	}
	vertices := string(files[GraphVerticesPath])
	if !strings.Contains(vertices, `"label":"CodeRef"`) || !strings.Contains(vertices, `"display":"Add@calc/calc.go:3"`) {
		t.Errorf("the graph has no readable CodeRef vertex:\n%s", vertices)
	}
}

func TestContextDocumentDoesNotRepeatTheSpec(t *testing.T) {
	files, err := Files(calcSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[ContextPath("calc")])
	if strings.Contains(document, clm.SpecFence) {
		t.Errorf("the context document repeats the spec block:\n%s", document)
	}
	if !strings.Contains(document, "Integer arithmetic.") || !strings.Contains(document, "function:abc") {
		t.Errorf("the context document lacks the prose or the managed refs:\n%s", document)
	}
}

func TestFeatureBranchWritesTheChangesDocument(t *testing.T) {
	snapshot := calcSnapshot()
	snapshot.Branch = "open-architecture/calc--spec-bob"
	baselineContext := calcSnapshot().Contexts[0]
	baselineContext.Spec.Requirements[0].Text = "Add returns the sum."
	snapshot.Baseline = &Baseline{
		Contexts: []spec.SystemContext{baselineContext},
		Changes:  []spec.SpecChange{{ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-a-b"}}},
	}
	snapshot.Contexts[0].Spec.Requirements = append(snapshot.Contexts[0].Spec.Requirements,
		spec.Requirement{ID: "r.sub", Level: spec.LevelMust, Text: "Subtract returns the difference."})
	snapshot.Contexts[0].Spec.Intent = "Integer arithmetic, with subtraction."
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	document, ok := files[ChangesDocPath]
	if !ok {
		t.Fatal("a feature branch has no CHANGES.md")
	}
	for _, want := range []string{
		"# Changes on `open-architecture/calc--spec-bob`",
		"### calc",
		"added `r.sub` (MUST): \"Subtract returns the difference.\"",
		"intent: \"Integer arithmetic.\" -> \"Integer arithmetic, with subtraction.\"",
		"| calc-s2c-12345678 | SpecToCode | Succeeded | c0ffee | 0 | - |",
	} {
		if !strings.Contains(string(document), want) {
			t.Errorf("CHANGES.md lacks %q:\n%s", want, document)
		}
	}
	if strings.Contains(string(document), "calc-c2s-a-b") {
		t.Errorf("CHANGES.md lists the baseline's own change as this branch's outcome:\n%s", document)
	}
}

func TestDefaultBranchWritesNoChangesDocument(t *testing.T) {
	files, err := Files(calcSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[ChangesDocPath]; ok {
		t.Fatal("the default branch carries a CHANGES.md")
	}
}

func TestStatusRecordsTheCommonCommitOnce(t *testing.T) {
	snapshot := calcSnapshot()
	snapshot.Contexts[0].Status.ObservedCommit = "aaaa"
	snapshot.Contexts[0].Status.SyncedCommit = "bbbb"
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	status := string(files[StatusPath("calc")])
	if strings.Contains(status, "observedCommit") || strings.Contains(status, "syncedCommit") {
		t.Errorf("the status file repeats the repository's commits:\n%s", status)
	}
	if !strings.Contains(string(files[RepositoryPath]), "observedCommit: aaaa") {
		t.Errorf("repository.yaml lacks the observed commit:\n%s", files[RepositoryPath])
	}
}

func TestStatusKeepsACommitThatDiffers(t *testing.T) {
	snapshot := calcSnapshot()
	snapshot.Contexts[0].Status.ObservedCommit = "aaaa"
	snapshot.Contexts[0].Status.SyncedCommit = "bbbb"
	other := snapshot.Contexts[0]
	other.Name = "other"
	other.Status.ObservedCommit = "cccc"
	snapshot.Contexts = append(snapshot.Contexts, other)
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files[StatusPath("calc")]), "observedCommit: aaaa") {
		t.Errorf("a context that differs lost its commit:\n%s", files[StatusPath("calc")])
	}
	if strings.Contains(string(files[RepositoryPath]), "observedCommit") {
		t.Errorf("repository.yaml claims a common commit:\n%s", files[RepositoryPath])
	}
}

func TestSubjectsNameEveryKindOfChange(t *testing.T) {
	snapshot := calcSnapshot()
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	previous := map[string][]byte{}
	for _, path := range []string{SpecPath("calc"), StatusPath("calc"), ChangePath("calc-s2c-12345678")} {
		previous[path] = files[path]
	}
	edited := snapshot
	edited.Contexts = append([]spec.SystemContext{}, snapshot.Contexts...)
	edited.Contexts[0].Spec.Requirements = append(edited.Contexts[0].Spec.Requirements,
		spec.Requirement{ID: "r.sub", Level: spec.LevelMust, Text: "Subtract."})
	edited.Contexts[0].Status.ObservedCommit = "1234567890"
	edited.Contexts[0].Status.Observed.Interfaces[0].Line = 9
	edited.Changes = append([]spec.SpecChange{}, snapshot.Changes...)
	edited.Changes[0].Status.Phase = specapi.PhaseRunning
	next, err := Files(edited)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanCommit(map[string]string{}, next)
	subjects := Subjects(plan, edited, previous)
	joined := strings.Join(subjects, "\n")
	for _, want := range []string{
		"spec(calc): +r.sub",
		"change(calc-s2c-12345678): Succeeded -> Running",
		"status(calc): observed 12345678",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("subjects lack %q:\n%s", want, joined)
		}
	}
}

func TestCoalesceProgressDefersAProgressOnlyRewrite(t *testing.T) {
	change := calcSnapshot().Changes[0]
	before, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name}, Spec: changeSpecDocOf(change.Spec), Status: changeStatusDocOf(change.Status)})
	if err != nil {
		t.Fatal(err)
	}
	after := change
	after.Status.Progress = append(after.Status.Progress, spec.ProgressRecord{Turn: 1, Tool: "edit", At: "2026-01-01T00:00:00Z"})
	next, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name}, Spec: changeSpecDocOf(change.Spec), Status: changeStatusDocOf(after.Status)})
	if err != nil {
		t.Fatal(err)
	}
	path := ChangePath(change.Name)
	plan := Plan{Write: map[string][]byte{path: next}, Modified: []string{path}}
	plan, deferred := CoalesceProgress(plan, map[string][]byte{path: before})
	if !plan.Empty() || len(deferred) != 1 || deferred[0] != path {
		t.Fatalf("a progress-only rewrite was planned: %+v %v", plan, deferred)
	}
	after.Status.Phase = specapi.PhaseFailed
	final, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name}, Spec: changeSpecDocOf(change.Spec), Status: changeStatusDocOf(after.Status)})
	if err != nil {
		t.Fatal(err)
	}
	plan = Plan{Write: map[string][]byte{path: final}, Modified: []string{path}}
	plan, deferred = CoalesceProgress(plan, map[string][]byte{path: before})
	if plan.Empty() || len(deferred) != 0 {
		t.Fatalf("a phase transition was deferred: %+v %v", plan, deferred)
	}
}

func TestCoalesceNoiseDefersACommitOnlyStatusRewrite(t *testing.T) {
	context := calcSnapshot().Contexts[0]
	common := commonCommitsDoc{ObservedCommit: "aaaa", SyncedCommit: "aaaa"}
	before := context.Status
	before.ObservedCommit = "aaaa"
	before.SyncedCommit = "aaaa"
	after := before
	after.ObservedCommit = "bbbb"
	after.SyncedCommit = "bbbb"
	oldDoc, err := yaml.Marshal(statusDocOf(before, common))
	if err != nil {
		t.Fatal(err)
	}
	newDoc, err := yaml.Marshal(statusDocOf(after, common))
	if err != nil {
		t.Fatal(err)
	}
	path := StatusPath(context.Name)
	plan := Plan{Write: map[string][]byte{path: newDoc}, Modified: []string{path}}
	plan, dropped := CoalesceNoise(plan, map[string][]byte{path: oldDoc})
	if !plan.Empty() || len(dropped) != 1 || dropped[0] != path {
		t.Fatalf("a commit-id-only status rewrite was planned: %+v %v", plan, dropped)
	}

	after.Conditions[0].Status = metav1.ConditionFalse
	realDoc, err := yaml.Marshal(statusDocOf(after, common))
	if err != nil {
		t.Fatal(err)
	}
	plan = Plan{Write: map[string][]byte{path: realDoc}, Modified: []string{path}}
	plan, dropped = CoalesceNoise(plan, map[string][]byte{path: oldDoc})
	if plan.Empty() || len(dropped) != 0 {
		t.Fatalf("a condition change was deferred: %+v %v", plan, dropped)
	}
}

func TestCoalesceNoiseDefersADerivedOnlySpecRewrite(t *testing.T) {
	context := calcSnapshot().Contexts[0]
	old, err := mirror.RenderWithRefs(context.Name, context.Namespace, context.Spec, []string{"function:abc Add@calc/calc.go:3"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := mirror.RenderWithRefs(context.Name, context.Namespace, context.Spec, []string{"function:abc Add@calc/calc.go:9"})
	if err != nil {
		t.Fatal(err)
	}
	path := SpecPath(context.Name)
	plan := Plan{Write: map[string][]byte{path: next}, Modified: []string{path}}
	plan, dropped := CoalesceNoise(plan, map[string][]byte{path: old})
	if !plan.Empty() || len(dropped) != 1 || dropped[0] != path {
		t.Fatalf("a derived-only spec rewrite was planned: %+v %v", plan, dropped)
	}

	edited := context.Spec
	edited.Intent = "Integer arithmetic, edited."
	real, err := mirror.RenderWithRefs(context.Name, context.Namespace, edited, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan = Plan{Write: map[string][]byte{path: real}, Modified: []string{path}}
	plan, dropped = CoalesceNoise(plan, map[string][]byte{path: old})
	if plan.Empty() || len(dropped) != 0 {
		t.Fatalf("a declared spec edit was deferred: %+v %v", plan, dropped)
	}
}

func TestChangesDocumentListsUnimplementedRequirements(t *testing.T) {
	snapshot := calcSnapshot()
	snapshot.Branch = "open-architecture/calc--spec-bob"
	snapshot.Baseline = &Baseline{}
	snapshot.Changes[0].Status.RequirementCoverage = []spec.RequirementVerdict{
		{ID: "r.add", Implemented: true, Evidence: "func Add"},
		{ID: "r.sub", Implemented: false, Evidence: "no Subtract in the diff"},
	}
	document := changesDocument(snapshot)
	for _, want := range []string{
		"| coverage |",
		"1 of 2 missing",
		"## Unimplemented requirements",
		"### calc-s2c-12345678",
		"`r.sub`",
		"no Subtract in the diff",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("CHANGES.md lacks %q:\n%s", want, document)
		}
	}
	_, section, _ := strings.Cut(document, "## Unimplemented requirements")
	if strings.Contains(section, "`r.add`") {
		t.Errorf("an implemented requirement is listed as unimplemented:\n%s", section)
	}
}

func TestChangesDocumentOmitsTheSectionWhenEveryRequirementIsImplemented(t *testing.T) {
	snapshot := calcSnapshot()
	snapshot.Branch = "open-architecture/calc--spec-bob"
	snapshot.Baseline = &Baseline{}
	snapshot.Changes[0].Status.RequirementCoverage = []spec.RequirementVerdict{{ID: "r.add", Implemented: true}}
	document := changesDocument(snapshot)
	if strings.Contains(document, "## Unimplemented requirements") {
		t.Errorf("an all-implemented change lists a section:\n%s", document)
	}
	if !strings.Contains(document, "1 implemented") {
		t.Errorf("the coverage column is missing:\n%s", document)
	}
}

func TestOneRecordPerEpisodeSummarizesTheEarlierAttempts(t *testing.T) {
	snapshot := calcSnapshot()
	failed := snapshot.Changes[0]
	failed.Name = "calc-s2c-12345678-a2"
	failed.Status = spec.SpecChangeStatus{Phase: specapi.PhaseFailed, Message: "verify exited 1"}
	succeeded := snapshot.Changes[0]
	succeeded.Status = spec.SpecChangeStatus{Phase: specapi.PhaseSucceeded, Commit: "c0ffee"}
	snapshot.Changes = []spec.SpecChange{failed, succeeded}
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[ChangePath("calc-s2c-12345678-a2")]; ok {
		t.Error("the failed attempt still has its own record")
	}
	surviving, ok := files[ChangePath("calc-s2c-12345678")]
	if !ok {
		t.Fatal("the surviving attempt has no record")
	}
	for _, want := range []string{"superseded:", "name: calc-s2c-12345678-a2", "phase: Failed", "message: verify exited 1"} {
		if !strings.Contains(string(surviving), want) {
			t.Errorf("the surviving record lacks %q:\n%s", want, surviving)
		}
	}
}

func TestPreserveChangesKeepsEveryRecordTheBranchHolds(t *testing.T) {
	change := calcSnapshot().Changes[0]
	survivor, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name}, Spec: changeSpecDocOf(change.Spec), Status: changeStatusDocOf(change.Status)})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name + "-a2"}, Spec: changeSpecDocOf(change.Spec), Status: changeStatusDocOf(spec.SpecChangeStatus{Phase: specapi.PhaseFailed})})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{SpecPath("calc"): []byte("spec"), ChangePath(change.Name): survivor}
	previous := map[string][]byte{
		ChangePath("calc-c2s-aaaaaaaaaaaa-bbbbbbbbbbbb"): []byte("kept"),
		ChangePath(change.Name + "-a2"):                  attempt,
		SpecPath("calc"):                                 []byte("old spec"),
	}
	PreserveChanges(files, previous)
	if string(files[ChangePath("calc-c2s-aaaaaaaaaaaa-bbbbbbbbbbbb")]) != "kept" {
		t.Error("an inherited change record was dropped")
	}
	if string(files[ChangePath(change.Name)]) != string(survivor) {
		t.Error("the rewritten change record was overwritten by the branch's copy")
	}
	if string(files[ChangePath(change.Name+"-a2")]) != string(attempt) {
		t.Error("the branch's superseded attempt was dropped: changes/ is append-only")
	}
	if string(files[SpecPath("calc")]) != "spec" {
		t.Error("a non-change file was preserved from the branch")
	}
}

func TestChangeRecordCarriesASummaryNotTheWholeDelta(t *testing.T) {
	snapshot := calcSnapshot()
	change := &snapshot.Changes[0]
	change.Spec.Delta = &spec.Delta{
		Intent: &spec.FieldDelta{From: "old", To: "new"},
		Requirements: []spec.RequirementDelta{
			{Op: spec.OpAdded, ID: "r.sub", To: &spec.Requirement{ID: "r.sub", Level: spec.LevelMust, Text: "Subtract returns the difference."}},
			{Op: spec.OpChanged, ID: "r.add", From: &spec.Requirement{ID: "r.add"}, To: &spec.Requirement{ID: "r.add", Text: "Add returns the sum of two integers."}},
		},
		Interfaces: []spec.InterfaceDelta{{Op: spec.OpRemoved, Name: "Multiply"}},
	}
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	record := string(files[ChangePath(change.Name)])
	for _, want := range []string{
		"direction: SpecToCode",
		"delta:",
		"counts:",
		"added: 1",
		"changed: 2",
		"- +r.sub",
		"- ~r.add",
		"- -Multiply",
	} {
		if !strings.Contains(record, want) {
			t.Errorf("the record lacks %q:\n%s", want, record)
		}
	}
	for _, unwanted := range []string{"Subtract returns the difference.", "from:", "to:"} {
		if strings.Contains(record, unwanted) {
			t.Errorf("the record still embeds the delta (%q):\n%s", unwanted, record)
		}
	}
	parsed, err := ChangeFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Spec.Direction != specapi.DirectionSpecToCode || parsed[0].Spec.SystemContext != "calc" {
		t.Fatalf("the compact record does not read back: %+v", parsed)
	}
}

func TestChangeRecordSummarizesProgressAndKeepsTheReport(t *testing.T) {
	snapshot := calcSnapshot()
	change := &snapshot.Changes[0]
	change.Status.Progress = []spec.ProgressRecord{
		{Tool: "Read", Files: []string{"calc/calc.go"}, At: "2026-10-04T21:05:24Z"},
		{Tool: "Read", Files: []string{"calc/more.go"}, At: "2026-10-04T21:05:26Z"},
		{Tool: "Edit", Files: []string{"calc/calc.go"}, At: "2026-10-04T21:05:37Z"},
		{Note: "turn", At: "2026-10-04T21:06:44Z"},
		{Turn: 3, Note: "turn", At: "2026-10-04T21:07:00Z"},
	}
	change.Status.AgentLog = "**Edited** calc.go.\n\nverify: exit 0, 13 ok"
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	record := string(files[ChangePath(change.Name)])
	for _, want := range []string{
		"progress:",
		"turns: 3",
		"tool: Edit",
		"count: 1",
		"tool: Read",
		"count: 2",
		"calc/more.go",
		"notes: 2",
		`first: "2026-10-04T21:05:24Z"`,
		`last: "2026-10-04T21:07:00Z"`,
		"**Edited** calc.go.",
	} {
		if !strings.Contains(record, want) {
			t.Errorf("the record lacks %q:\n%s", want, record)
		}
	}
	if strings.Count(record, "2026-10-04T21:0") != 2 {
		t.Errorf("the record lists one timestamp per turn, not a first and a last:\n%s", record)
	}
	if strings.Count(record, "- tool:") != 2 {
		t.Errorf("the record lists one entry per tool call, not one count per tool:\n%s", record)
	}
	parsed, err := ChangeFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Status.Phase != specapi.PhaseSucceeded {
		t.Fatalf("the summarized record does not read back: %+v", parsed)
	}
}

func TestABranchWrittenWithTheFullDeltaStillLoads(t *testing.T) {
	legacy := `apiVersion: spec.graph-clm.dev/v1alpha1
kind: SpecChange
metadata:
  name: calc-s2c-12345678
spec:
  systemContext: calc
  direction: SpecToCode
  toSpecHash: abc
  delta:
    requirements:
    - op: added
      id: r.sub
      to:
        id: r.sub
        level: MUST
        text: Subtract returns the difference.
status:
  phase: Succeeded
  commit: c0ffee
`
	files := map[string][]byte{ChangePath("calc-s2c-12345678"): []byte(legacy)}
	parsed, err := ChangeFiles(files)
	if err != nil {
		t.Fatalf("a branch written before the summary does not load: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Name != "calc-s2c-12345678" || parsed[0].Spec.Direction != specapi.DirectionSpecToCode {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed[0].Status.Phase != specapi.PhaseSucceeded || parsed[0].Status.Commit != "c0ffee" {
		t.Fatalf("the status was lost: %+v", parsed[0].Status)
	}
}

func TestSpecFilesAndRepositoryFileRoundTrip(t *testing.T) {
	snapshot := calcSnapshot()
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := SpecFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := specs["calc"]
	if !ok || got.Spec.Intent != "Integer arithmetic." || len(got.Spec.Requirements) != 1 {
		t.Fatalf("spec files = %+v", specs)
	}
	repository, err := RepositoryFile(files)
	if err != nil {
		t.Fatal(err)
	}
	if repository.Name != "calc" || repository.Spec.Source == nil || repository.Spec.Source.Git.URL != "https://example.com/calc.git" {
		t.Fatalf("repository = %+v", repository)
	}
}

func TestABranchWrittenBeforeThisChangeStillLoads(t *testing.T) {
	old := map[string][]byte{
		RepositoryPath: []byte(`apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Repository
metadata:
  name: calc
  namespace: default
spec:
  branch: main
status:
  headCommit: aaaa
  phase: Populated
`),
		SpecPath("calc"): []byte(`apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: SystemContext
metadata:
  name: calc
  namespace: default
spec:
  repository: calc
  upstream: self
  intent: Integer arithmetic.
  requirements:
  - id: r.add
    level: MUST
    text: Add returns the sum.
`),
		StatusPath("calc"): []byte(`observedCommit: aaaa
syncedCommit: aaaa
observed:
  files:
  - calc/calc.go
`),
		ChangePath("calc-c2s-aaaa-bbbb"): []byte(`apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: SpecChange
metadata:
  name: calc-c2s-aaaa-bbbb
  namespace: default
spec:
  systemContext: calc
  direction: CodeToSpec
  fromCommit: aaaa
  toCommit: bbbb
status:
  phase: Succeeded
`),
		ArchPath: []byte("apiVersion: open-architecture.dffml.github.io/v0alpha1\nkind: OpenArchitecture\nmetadata:\n  name: calc\n  branch: open-architecture/calc\n"),
	}
	specs, err := SpecFiles(old)
	if err != nil {
		t.Fatal(err)
	}
	if specs["calc"].Spec.Intent != "Integer arithmetic." {
		t.Fatalf("specs = %+v", specs)
	}
	repository, err := RepositoryFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if repository.Name != "calc" || repository.Spec.Branch != "main" {
		t.Fatalf("repository = %+v", repository)
	}
	changes, err := ChangeFiles(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Status.Phase != specapi.PhaseSucceeded {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestMergeTakesABranchEditWhenKcpDidNotMove(t *testing.T) {
	base := calcSnapshot().Contexts[0].Spec
	theirs := base
	theirs.Requirements = append(append([]spec.Requirement{}, base.Requirements...), spec.Requirement{ID: "r.sub", Level: spec.LevelMust, Text: "Subtract returns the difference."})
	result := Merge(base, base, theirs)
	if !result.Changed || len(result.Spec.Requirements) != 2 || len(result.Conflicts) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Spec.CodeRefs) != 1 {
		t.Fatalf("the merge dropped kcp's derived refs: %v", result.Spec.CodeRefs)
	}
}

func TestMergeKeepsBothSidesOnDisjointKeys(t *testing.T) {
	base := calcSnapshot().Contexts[0].Spec
	ours := base
	ours.Intent = "Integer arithmetic, changed in kcp."
	theirs := base
	theirs.Interfaces = append(append([]spec.Interface{}, base.Interfaces...), spec.Interface{Name: "Subtract", Kind: "function"})
	result := Merge(base, ours, theirs)
	if len(result.Conflicts) != 0 || result.Spec.Intent != ours.Intent || len(result.Spec.Interfaces) != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestMergeRefusesTheSameKeyChangedTwoWays(t *testing.T) {
	base := calcSnapshot().Contexts[0].Spec
	ours := base
	ours.Intent = "kcp says this"
	theirs := base
	theirs.Intent = "the branch says that"
	result := Merge(base, ours, theirs)
	if strings.Join(result.Conflicts, ",") != "intent" || result.Spec.Intent != ours.Intent {
		t.Fatalf("result = %+v", result)
	}
	same := Merge(base, ours, ours)
	if len(same.Conflicts) != 0 {
		t.Fatalf("the same edit on both sides conflicts: %+v", same)
	}
}

func TestBranchNames(t *testing.T) {
	if Branch("calc") != "open-architecture/calc" || Ref("calc") != "refs/heads/open-architecture/calc" {
		t.Fatal(Branch("calc"), Ref("calc"))
	}
}

func TestBranchForFollowsTheCodeBranch(t *testing.T) {
	cases := []struct{ code, def, want string }{
		{"main", "main", "open-architecture/deno-kcp"},
		{"", "main", "open-architecture/deno-kcp"},
		{"master", "master", "open-architecture/deno-kcp"},
		{"spec/bidder-and-bob-pds", "main", "open-architecture/deno-kcp--spec-bidder-and-bob-pds"},
		{"feature/x.y", "main", "open-architecture/deno-kcp--feature-x.y"},
	}
	for _, c := range cases {
		if got := BranchFor("deno-kcp", c.code, c.def); got != c.want {
			t.Errorf("BranchFor(%q, %q) = %q, want %q", c.code, c.def, got, c.want)
		}
	}
	if RefFor("deno-kcp", "spec/a", "main") != "refs/heads/open-architecture/deno-kcp--spec-a" {
		t.Fatal(RefFor("deno-kcp", "spec/a", "main"))
	}
}

func TestChangeHistoryExpandsTheSupersededAttempts(t *testing.T) {
	document := `
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: SpecChange
metadata: {name: calc-s2c-aaaa-a3, namespace: default}
spec:
  systemContext: calc
  direction: SpecToCode
  toSpecHash: aaaa
status:
  phase: Succeeded
  commit: c0ffee
superseded:
  - {name: calc-s2c-aaaa-a2, phase: Failed, commit: deadbeef, message: "verify exited 1"}
`
	files := map[string][]byte{"changes/calc-s2c-aaaa-a3.yaml": []byte(document)}

	surviving, err := ChangeFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(surviving) != 1 || surviving[0].Name != "calc-s2c-aaaa-a3" {
		t.Fatalf("ChangeFiles = %+v, want only the surviving attempt", surviving)
	}

	history, err := ChangeHistory(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("ChangeHistory = %+v, want the survivor and the superseded attempt", history)
	}
	older := history[1]
	if older.Name != "calc-s2c-aaaa-a2" || older.Status.Phase != specapi.PhaseFailed || older.Status.Commit != "deadbeef" {
		t.Fatalf("superseded attempt = %+v", older)
	}
	if older.Spec.SystemContext != "calc" || older.Spec.ToSpecHash != "aaaa" {
		t.Fatalf("superseded attempt lost the episode's spec: %+v", older.Spec)
	}
}

// TestFeatureBranchMarksAGuardedRequirement pins the requirement linkage: a
// requirement a constraint template enforces is marked in CHANGES.md.
func TestFeatureBranchMarksAGuardedRequirement(t *testing.T) {
	snapshot := calcSnapshot()
	snapshot.Branch = "open-architecture/calc--spec-bob"
	snapshot.Baseline = &Baseline{Contexts: calcSnapshot().Contexts}
	snapshot.Contexts[0].Spec.Requirements = append(snapshot.Contexts[0].Spec.Requirements,
		spec.Requirement{ID: "r.sub", Level: spec.LevelMust, Text: "Subtract returns the difference."})
	snapshot.Guarded = map[string][]string{"calc": {"r.sub"}}
	files, err := Files(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	document := string(files[ChangesDocPath])
	if !strings.Contains(document, "added `r.sub` (MUST): \"Subtract returns the difference.\" (policy-guarded)") {
		t.Errorf("CHANGES.md does not mark the guarded requirement:\n%s", document)
	}
	unguarded := calcSnapshot()
	unguarded.Branch = "open-architecture/calc--spec-bob"
	unguarded.Baseline = &Baseline{Contexts: calcSnapshot().Contexts}
	unguarded.Contexts[0].Spec.Requirements = append(unguarded.Contexts[0].Spec.Requirements,
		spec.Requirement{ID: "r.sub", Level: spec.LevelMust, Text: "Subtract returns the difference."})
	plain, err := Files(unguarded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain[ChangesDocPath]), "policy-guarded") {
		t.Errorf("an unguarded requirement is marked:\n%s", plain[ChangesDocPath])
	}
}
