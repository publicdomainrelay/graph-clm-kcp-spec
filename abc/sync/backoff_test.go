package specsync

import (
	"testing"
	"time"
)

func TestRetryBackoffRaisesTheFirstAttemptAtOnce(t *testing.T) {
	wait, ready := RetryBackoff(0, time.Time{}, time.Now(), 10*time.Second, 3)
	if !ready || wait != 0 {
		t.Fatalf("wait = %s ready = %v, want the first attempt now", wait, ready)
	}
}

func TestRetryBackoffRaisesARetryWhenTheFailureTimeIsUnknown(t *testing.T) {
	wait, ready := RetryBackoff(1, time.Time{}, time.Now(), 10*time.Second, 3)
	if !ready || wait != 0 {
		t.Fatalf("wait = %s ready = %v, want an immediate retry", wait, ready)
	}
}

func TestRetryBackoffWaitsLongerAfterEachFailure(t *testing.T) {
	now := time.Now()
	first, ready := RetryBackoff(1, now, now, 10*time.Second, 5)
	if !ready || first != 10*time.Second {
		t.Fatalf("first wait = %s (ready %v), want 10s", first, ready)
	}
	second, ready := RetryBackoff(2, now, now, 10*time.Second, 5)
	if !ready || second != 20*time.Second {
		t.Fatalf("second wait = %s (ready %v), want 20s", second, ready)
	}
	third, ready := RetryBackoff(3, now, now, 10*time.Second, 5)
	if !ready || third != 40*time.Second {
		t.Fatalf("third wait = %s (ready %v), want 40s", third, ready)
	}
}

func TestRetryBackoffStopsAtTheCap(t *testing.T) {
	now := time.Now()
	if _, ready := RetryBackoff(3, now, now, time.Second, 3); ready {
		t.Fatal("the fourth attempt was allowed past a cap of three")
	}
	if _, ready := RetryBackoff(9, now, now, time.Second, 0); !ready {
		t.Fatal("a cap of zero must mean no cap")
	}
}

func TestRetryBackoffReleasesOnceTheWaitHasPassed(t *testing.T) {
	now := time.Now()
	wait, ready := RetryBackoff(2, now.Add(-30*time.Second), now, 10*time.Second, 5)
	if !ready || wait != 0 {
		t.Fatalf("wait = %s ready = %v, want ready", wait, ready)
	}
	partial, ready := RetryBackoff(2, now.Add(-5*time.Second), now, 10*time.Second, 5)
	if !ready || partial <= 0 || partial > 15*time.Second {
		t.Fatalf("partial wait = %s (ready %v), want the rest of the second wait", partial, ready)
	}
}
