package spec

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func retryChange(name, phase, context, direction, hash string, created int64) SpecChange {
	return SpecChange{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.Unix(created, 0),
		},
		Spec: SpecChangeSpec{
			SystemContext: context,
			Direction:     direction,
			ToSpecHash:    hash,
		},
		Status: SpecChangeStatus{Phase: phase},
	}
}

func TestRetryAttemptsNamesTheNextAttemptOverTheKeptFailures(t *testing.T) {
	base := ChangeNameSpecToCode("calc", "aaaa")
	changes := []SpecChange{
		retryChange(base, specapi.PhaseFailed, "calc", specapi.DirectionSpecToCode, "aaaa", 1),
		retryChange(base+"-a2", specapi.PhaseFailed, "calc", specapi.DirectionSpecToCode, "aaaa", 2),
		retryChange("other-s2c-bbbb", specapi.PhaseFailed, "other", specapi.DirectionSpecToCode, "bbbb", 3),
	}
	attempts := RetryAttempts(changes, "calc")
	if len(attempts) != 1 {
		t.Fatalf("attempts = %+v", attempts)
	}
	if attempts[0].Name != base+"-a3" || attempts[0].Attempt != 3 {
		t.Errorf("next attempt = %+v, want %s at attempt 3", attempts[0], base+"-a3")
	}
	if attempts[0].Source.Name != base+"-a2" {
		t.Errorf("the source must be the newest failure, got %q", attempts[0].Source.Name)
	}
}

func TestRetryAttemptsLeavesSettledAndRunningEpisodesAlone(t *testing.T) {
	base := ChangeNameSpecToCode("calc", "aaaa")
	succeeded := []SpecChange{retryChange(base, specapi.PhaseSucceeded, "calc", specapi.DirectionSpecToCode, "aaaa", 1)}
	if got := RetryAttempts(succeeded, "calc"); len(got) != 0 {
		t.Errorf("a succeeded episode was retried: %+v", got)
	}
	running := []SpecChange{retryChange(base+"-a2", specapi.PhaseRunning, "calc", specapi.DirectionSpecToCode, "aaaa", 1)}
	if got := RetryAttempts(running, "calc"); len(got) != 0 {
		t.Errorf("a running episode was retried: %+v", got)
	}
	recovered := []SpecChange{
		retryChange(base, specapi.PhaseFailed, "calc", specapi.DirectionSpecToCode, "aaaa", 1),
		retryChange(base+"-a2", specapi.PhaseSucceeded, "calc", specapi.DirectionSpecToCode, "aaaa", 2),
	}
	if got := RetryAttempts(recovered, "calc"); len(got) != 0 {
		t.Errorf("an episode that later succeeded was retried: %+v", got)
	}
	empty := []SpecChange{retryChange(base, "", "calc", specapi.DirectionSpecToCode, "aaaa", 1)}
	if got := RetryAttempts(empty, "calc"); len(got) != 0 {
		t.Errorf("an unsettled episode was retried: %+v", got)
	}
}

func TestRetryAttemptsCoversBothDirections(t *testing.T) {
	c2s := ChangeNameCodeToSpec("calc", "aaaa", "bbbb")
	s2c := ChangeNameSpecToCode("calc", "cccc")
	changes := []SpecChange{
		retryChange(s2c, specapi.PhaseFailed, "calc", specapi.DirectionSpecToCode, "cccc", 2),
		retryChange(c2s, specapi.PhaseFailed, "calc", specapi.DirectionCodeToSpec, "dddd", 1),
	}
	changes[1].Spec.FromCommit = "aaaa"
	changes[1].Spec.ToCommit = "bbbb"
	attempts := RetryAttempts(changes, "calc")
	if len(attempts) != 2 {
		t.Fatalf("attempts = %+v", attempts)
	}
	if attempts[0].Base != c2s || attempts[1].Base != s2c {
		t.Errorf("attempts must be ordered by base: %+v", attempts)
	}
}
