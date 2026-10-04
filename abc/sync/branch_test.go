package specsync

import "testing"

func TestBranchCheck(t *testing.T) {
	cases := []struct {
		name  string
		head  string
		want  string
		match bool
	}{
		{"the same branch", "feature", "feature", false},
		{"another branch", "main", "feature", true},
		{"a detached head", "HEAD", "feature", true},
		{"an unknown head", "", "feature", true},
		{"no branch recorded", "main", "", false},
	}
	for _, testCase := range cases {
		status := BranchCheck(testCase.head, testCase.want)
		if status.Mismatch != testCase.match {
			t.Errorf("%s: mismatch = %v, want %v", testCase.name, status.Mismatch, testCase.match)
		}
		if status.Mismatch && status.Message == "" {
			t.Errorf("%s: a mismatch carries no message", testCase.name)
		}
	}
}
