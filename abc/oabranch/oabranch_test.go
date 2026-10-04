package oabranch

import (
	"os/exec"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
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
	for _, path := range []string{ReadmePath, RepositoryPath, ArchPath, SpecPath("calc"), StatusPath("calc"), ContextPath("calc"), ChangePath("calc-s2c-12345678"), GraphVerticesPath, GraphEdgesPath} {
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
	if !strings.Contains(string(files[ArchPath]), "id: sc.calc") || !strings.Contains(string(files[ArchPath]), "Integer arithmetic.") {
		t.Errorf("arch.yaml lacks the context:\n%s", files[ArchPath])
	}
	if !strings.Contains(string(files[ContextPath("calc")]), "function:abc") {
		t.Errorf("context document lacks the resolved ref:\n%s", files[ContextPath("calc")])
	}
	if !strings.Contains(string(files[GraphVerticesPath]), `"label":"SpecContext"`) {
		t.Errorf("vertices lack the context:\n%s", files[GraphVerticesPath])
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
	if !strings.Contains(specFile, "codeRefIndex:") || !strings.Contains(specFile, "function:abc: Add@calc/calc.go:3") {
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
	before, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name}, Spec: change.Spec, Status: change.Status})
	if err != nil {
		t.Fatal(err)
	}
	after := change
	after.Status.Progress = append(after.Status.Progress, spec.ProgressRecord{Turn: 1, Tool: "edit", At: "2026-01-01T00:00:00Z"})
	next, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name}, Spec: change.Spec, Status: after.Status})
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
	final, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name}, Spec: change.Spec, Status: after.Status})
	if err != nil {
		t.Fatal(err)
	}
	plan = Plan{Write: map[string][]byte{path: final}, Modified: []string{path}}
	plan, deferred = CoalesceProgress(plan, map[string][]byte{path: before})
	if plan.Empty() || len(deferred) != 0 {
		t.Fatalf("a phase transition was deferred: %+v %v", plan, deferred)
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

func TestPreserveChangesKeepsTheBranchesOwnRecords(t *testing.T) {
	change := calcSnapshot().Changes[0]
	survivor, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name}, Spec: change.Spec, Status: change.Status})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := yaml.Marshal(changeDoc{Metadata: objectMeta{Name: change.Name + "-a2"}, Spec: change.Spec, Status: spec.SpecChangeStatus{Phase: specapi.PhaseFailed}})
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
	if _, ok := files[ChangePath(change.Name+"-a2")]; ok {
		t.Error("a superseded attempt came back from the branch")
	}
	if string(files[SpecPath("calc")]) != "spec" {
		t.Error("a non-change file was preserved from the branch")
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
