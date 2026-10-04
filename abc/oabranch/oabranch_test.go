package oabranch

import (
	"os/exec"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
			{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum.", CodeRefs: []string{"file:calc/calc.go"}},
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
	message := Message("calc", plan, objects, commits)
	for _, want := range []string{
		"open-architecture: calc: ",
		"A specs/calc.yaml",
		"SystemContext calc generation=3 resourceVersion=42 origin=clm",
		"SpecChange calc-s2c-12345678",
		"Code-Commit: c0ffee",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("message lacks %q:\n%s", want, message)
		}
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
