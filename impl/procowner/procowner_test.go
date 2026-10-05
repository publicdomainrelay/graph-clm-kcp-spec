package procowner

import (
	"os"
	"testing"
	"time"
)

func TestAliveNamesTheRunningProcessAndNoOneElse(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Error("the current process is not alive")
	}
	if Alive(0) || Alive(-1) {
		t.Error("a pid of zero or less is not a live owner")
	}
}

func TestOwnsIsTokenEqualityAndNeverTheEmptyToken(t *testing.T) {
	owner := Owner{Token: "abc", Pid: 1, Since: time.Now()}
	if !owner.Owns("abc") {
		t.Error("the owner does not own its own token")
	}
	if owner.Owns("") || owner.Owns("other") {
		t.Error("the owner owns a token it does not carry")
	}
}

func TestOrphanedDecidesByOwnerAndLiveness(t *testing.T) {
	mine := Owner{Token: "mine", Pid: 1, Since: time.Now()}
	dead := func(int) bool { return false }
	live := func(int) bool { return true }
	cases := []struct {
		name  string
		token string
		pid   int
		alive func(int) bool
		want  bool
	}{
		{"this process's own token", "mine", 2, dead, false},
		{"another token whose process is gone", "other", 2, dead, true},
		{"another token whose process is alive", "other", 2, live, false},
		{"no token at all", "", 0, live, true},
	}
	for _, testCase := range cases {
		if got := Orphaned(mine, testCase.token, testCase.pid, testCase.alive); got != testCase.want {
			t.Errorf("%s: orphaned = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestFieldsAndClearedNameTheSameKeys(t *testing.T) {
	owner := Owner{Token: "abc", Pid: 7, Since: time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)}
	fields := owner.Fields()
	if fields["owner"] != "abc" || fields["ownerPid"] != 7 || fields["ownerStartedAt"] != "2026-10-05T01:02:03Z" {
		t.Fatalf("fields = %v", fields)
	}
	cleared := Cleared()
	for key := range fields {
		value, found := cleared[key]
		if !found || value != nil {
			t.Errorf("cleared[%s] = %v, want nil", key, value)
		}
	}
}
