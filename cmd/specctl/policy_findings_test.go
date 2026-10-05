package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireCodegraph(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
}

func findingsLibrary(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "..", "examples", "policies", "market-mini")
	target := filepath.Join(t.TempDir(), "market-mini")
	if err := os.CopyFS(target, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	return target
}

func violatingFixture(t *testing.T) string {
	t.Helper()
	absolute, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "market-mini", "violating"))
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

func runFindings(t *testing.T, library string, extra ...string) []Finding {
	t.Helper()
	args := append([]string{"policy", "findings", "--repo", "market-mini",
		"--worktree", violatingFixture(t), "--library", library, "-o", "json"}, extra...)
	code, stdout, stderr := runWith(args...)
	if code != exitOK {
		t.Fatalf("findings: code %d, stderr %q", code, stderr)
	}
	var findings []Finding
	if err := json.Unmarshal([]byte(stdout), &findings); err != nil {
		t.Fatalf("findings output is not JSON: %v\n%s", err, stdout)
	}
	return findings
}

func TestPolicyFindingsListsEveryViolationWithAKey(t *testing.T) {
	requireCodegraph(t)
	findings := runFindings(t, findingsLibrary(t))
	if len(findings) == 0 {
		t.Fatal("the violating fixture reported no finding")
	}
	byStatus := map[string]int{}
	for _, finding := range findings {
		byStatus[finding.Status]++
		if finding.Key == "" || len(finding.Key) != 16 {
			t.Errorf("finding %+v has no stable key", finding)
		}
		if finding.Constraint == "" || finding.Object == "" || finding.Message == "" {
			t.Errorf("finding %+v is missing part of its identity", finding)
		}
	}
	if byStatus[findingNew] != len(findings) {
		t.Errorf("without a base every finding is new: %v", byStatus)
	}
}

func TestPolicyWaiveRecordsAnExceptionAndFindingsReportItWaived(t *testing.T) {
	requireCodegraph(t)
	library := findingsLibrary(t)
	findings := runFindings(t, library)
	key := findings[0].Key

	code, stdout, stderr := runWith("policy", "waive", key,
		"--reason", "accepted for this measurement",
		"--owner", "tester",
		"--repo", "market-mini", "--worktree", violatingFixture(t), "--library", library, "--dir", library)
	if code != exitOK {
		t.Fatalf("waive: code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "waived") {
		t.Errorf("waive stdout = %q", stdout)
	}
	written := filepath.Join(library, "exceptions", key+".yaml")
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("the exception was not written: %v", err)
	}

	after := runFindings(t, library)
	waived := 0
	for _, finding := range after {
		if finding.Key == key {
			if finding.Status != findingWaived {
				t.Errorf("%s is %s after the waiver, want waived", key, finding.Status)
			}
			if finding.Reason != "accepted for this measurement" || finding.Owner != "tester" {
				t.Errorf("the waiver lost its reason or owner: %+v", finding)
			}
			waived++
		}
	}
	if waived != 1 {
		t.Errorf("the waived finding appears %d times, want once", waived)
	}
}

func TestPolicyWaiveRefusesWithoutAReason(t *testing.T) {
	code, _, stderr := runWith("policy", "waive", "0000000000000000", "--repo", "market-mini")
	if code != exitUsage {
		t.Fatalf("code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "--reason is required") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestPolicyFixTurnsAFindingIntoASpecChangeRequest(t *testing.T) {
	requireCodegraph(t)
	findings := runFindings(t, findingsLibrary(t))
	target := findings[0]

	code, stdout, stderr := runWith("policy", "fix", target.Key,
		"--repo", "market-mini", "--worktree", violatingFixture(t),
		"--library", findingsLibrary(t), "-o", "json")
	if code != exitOK {
		t.Fatalf("fix: code %d, stderr %q", code, stderr)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(stdout), &request); err != nil {
		t.Fatalf("the request is not readable JSON: %v\n%s", err, stdout)
	}
	for _, field := range []string{"repository", "constraint", "object", "message", "prompt"} {
		if value, _ := request[field].(string); value == "" {
			t.Errorf("the request carries no %s: %s", field, stdout)
		}
	}
	if request["constraint"] != target.Constraint {
		t.Errorf("constraint = %v, want %s", request["constraint"], target.Constraint)
	}
	if !strings.Contains(request["prompt"].(string), target.Message) {
		t.Errorf("the prompt does not carry the violation message: %v", request["prompt"])
	}
}
