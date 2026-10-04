package specsync

import (
	"testing"
	"time"

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
