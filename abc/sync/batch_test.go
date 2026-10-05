package specsync

import (
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func changeRef(name, repository, phase string, at time.Time) ChangeRef {
	return ChangeRef{
		Name:          name,
		SystemContext: name,
		Repository:    repository,
		Direction:     specapi.DirectionSpecToCode,
		Phase:         phase,
		CreatedAt:     at,
	}
}

func TestPendingForRepositoryKeepsOnlyItsPendingSpecToCodeChanges(t *testing.T) {
	at := time.Unix(100, 0)
	changes := []ChangeRef{
		changeRef("calc-a", "calc", specapi.PhasePending, at),
		changeRef("calc-b", "calc", specapi.PhaseRunning, at),
		changeRef("other-a", "other", specapi.PhasePending, at),
		{Name: "calc-c2s", SystemContext: "calc", Repository: "calc", Direction: specapi.DirectionCodeToSpec, Phase: specapi.PhasePending, CreatedAt: at},
	}
	pending := PendingForRepository(changes, "calc")
	if len(pending) != 1 || pending[0].Name != "calc-a" {
		t.Fatalf("pending = %+v, want only calc-a", pending)
	}
}

func TestBatchMembersOrderOldestFirstAndBreakTiesByName(t *testing.T) {
	base := time.Unix(100, 0)
	changes := []ChangeRef{
		changeRef("calc-z", "calc", specapi.PhasePending, base.Add(2*time.Second)),
		changeRef("calc-b", "calc", specapi.PhasePending, base),
		changeRef("calc-a", "calc", specapi.PhasePending, base),
		changeRef("calc-y", "calc", specapi.PhasePending, base.Add(time.Second)),
	}
	ordered := BatchMembers(changes)
	got := []string{}
	for _, change := range ordered {
		got = append(got, change.Name)
	}
	want := []string{"calc-a", "calc-b", "calc-y", "calc-z"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	leader, ok := BatchLeader(changes)
	if !ok || leader.Name != "calc-a" {
		t.Fatalf("leader = %+v, want calc-a", leader)
	}
	if _, ok := BatchLeader(nil); ok {
		t.Fatal("an empty set has a leader")
	}
}

func TestBatchGatherWaitRunsFromTheOldestPendingChange(t *testing.T) {
	led := changeRef("calc-a", "calc", specapi.PhasePending, time.Unix(100, 0))
	window := 5 * time.Second
	if wait := BatchGatherWait(led, time.Unix(102, 0), window); wait != 3*time.Second {
		t.Errorf("wait = %s, want the rest of the window", wait)
	}
	if wait := BatchGatherWait(led, time.Unix(106, 0), window); wait != 0 {
		t.Errorf("wait = %s, want the window elapsed", wait)
	}
	if wait := BatchGatherWait(led, time.Unix(101, 0), 0); wait != 0 {
		t.Errorf("wait = %s, want no wait without a window", wait)
	}
}

func TestRepositoryBusyLooksAtEveryChangeOfTheRepository(t *testing.T) {
	at := time.Unix(100, 0)
	running := changeRef("calc-a", "calc", specapi.PhaseRunning, at)
	if !RepositoryBusy([]ChangeRef{running}, "calc") {
		t.Error("a running spec to code change did not make the repository busy")
	}
	pending := changeRef("calc-a", "calc", specapi.PhasePending, at)
	if RepositoryBusy([]ChangeRef{pending}, "calc") {
		t.Error("a pending change made the repository busy")
	}
	if RepositoryBusy([]ChangeRef{running}, "other") {
		t.Error("another repository was reported busy")
	}
	codeToSpec := ChangeRef{Name: "calc-c2s", Repository: "calc", Direction: specapi.DirectionCodeToSpec, Phase: specapi.PhaseRunning, CreatedAt: at}
	if RepositoryBusy([]ChangeRef{codeToSpec}, "calc") {
		t.Error("a running code to spec change made the repository busy")
	}
}

func TestSiblingLandedOnlyCountsAnotherCommittedChange(t *testing.T) {
	at := time.Unix(100, 0)
	sibling := changeRef("calc-b", "calc", specapi.PhaseSucceeded, at)
	sibling.Commit = "head"
	own := changeRef("calc-a", "calc", specapi.PhaseSucceeded, at)
	own.Commit = "head"
	changes := []ChangeRef{sibling, own}

	if !SiblingLanded(changes, "calc", "base", "head", []string{"calc-a"}) {
		t.Error("a sibling that landed the head commit was not seen")
	}
	if SiblingLanded(changes, "calc", "base", "head", []string{"calc-a", "calc-b"}) {
		t.Error("the batch's own members were counted as siblings")
	}
	if SiblingLanded(changes, "calc", "head", "head", []string{"calc-a"}) {
		t.Error("a branch that did not move counted as a sibling landing")
	}
	if SiblingLanded(changes, "calc", "base", "other", []string{"calc-a"}) {
		t.Error("a commit nobody recorded counted as a sibling landing")
	}
}

func TestOrderBatchPutsADependencyBeforeItsDependent(t *testing.T) {
	at := time.Unix(100, 0)
	calc := changeRef("calc", "repo", specapi.PhasePending, at)
	calc.DependsOn = []string{"sc.cmd-calc"}
	cli := changeRef("cmd-calc", "repo", specapi.PhasePending, at.Add(time.Second))
	ordered := OrderBatch([]ChangeRef{calc, cli})
	if ordered[0].Name != "cmd-calc" || ordered[1].Name != "calc" {
		t.Fatalf("order = %v, want the dependency first", names(ordered))
	}
}

func TestOrderBatchKeepsIndependentMembersAndBreaksCycles(t *testing.T) {
	at := time.Unix(100, 0)
	first := changeRef("a", "repo", specapi.PhasePending, at)
	second := changeRef("b", "repo", specapi.PhasePending, at.Add(time.Second))
	if ordered := OrderBatch([]ChangeRef{first, second}); ordered[0].Name != "a" || ordered[1].Name != "b" {
		t.Fatalf("independent order = %v, want the input order", names(ordered))
	}
	first.DependsOn = []string{"sc.b"}
	second.DependsOn = []string{"sc.a"}
	ordered := OrderBatch([]ChangeRef{first, second})
	if len(ordered) != 2 {
		t.Fatalf("a cycle dropped a member: %v", names(ordered))
	}
}

func TestOrderBatchIgnoresRefsThatAreNotMembers(t *testing.T) {
	at := time.Unix(100, 0)
	calc := changeRef("calc", "repo", specapi.PhasePending, at)
	calc.DependsOn = []string{"sc.elsewhere", "self", "sc."}
	cli := changeRef("cli", "repo", specapi.PhasePending, at.Add(time.Second))
	ordered := OrderBatch([]ChangeRef{calc, cli})
	if ordered[0].Name != "calc" || ordered[1].Name != "cli" {
		t.Fatalf("order = %v, want the input order", names(ordered))
	}
}

func TestFilesOwnedElsewhereNamesOnlyAnotherContextsFiles(t *testing.T) {
	observed := map[string]spec.ObservedFacts{
		"calc":     {Files: []string{"calc/calc.go"}},
		"registry": {Files: []string{"registry/registry.go"}, TreeFiles: []string{"registry/README.md"}},
	}
	outside := FilesOwnedElsewhere(
		[]string{"calc/calc.go", "registry/registry.go", "registry/README.md", "calc/new.go"},
		[]string{"calc"}, observed)
	if len(outside) != 2 {
		t.Fatalf("outside = %+v", outside)
	}
	if owners := outside["registry/registry.go"]; len(owners) != 1 || owners[0] != "registry" {
		t.Errorf("registry/registry.go owners = %v", owners)
	}
	if _, found := outside["calc/new.go"]; found {
		t.Error("a new file no context observes must not be flagged")
	}
	if _, found := outside["calc/calc.go"]; found {
		t.Error("a member's own file must not be flagged")
	}
}

func names(changes []ChangeRef) []string {
	out := make([]string, 0, len(changes))
	for _, change := range changes {
		out = append(out, change.Name)
	}
	return out
}
