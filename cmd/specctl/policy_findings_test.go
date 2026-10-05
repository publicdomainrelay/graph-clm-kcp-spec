package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
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

// A durable exception is honoured by the plain offline evaluation, not only by
// `findings` and `--inherited` (review 0006 N7).
func TestPolicyEvalHonoursADurableException(t *testing.T) {
	requireCodegraph(t)
	library := findingsLibrary(t)
	findings := runFindings(t, library)
	if len(findings) < 2 {
		t.Skip("the fixture carries too few findings to waive them all")
	}
	// The waive verb is exercised by its own test; here the exceptions are
	// written directly so the whole fixture can be waived in one run.
	directory := filepath.Join(library, "exceptions")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		body := "constraint: " + finding.Constraint + "\nkey: " + finding.Key +
			"\nobject: " + finding.Object + "\nreason: accepted for this measurement\n"
		if err := os.WriteFile(filepath.Join(directory, finding.Key+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	code, stdout, stderr := runWith("policy", "eval", "--repo", "market-mini",
		"--worktree", violatingFixture(t), "--library", library)
	if code != exitOK {
		t.Fatalf("eval: code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "waived") {
		t.Errorf("eval does not report a violation as waived:\n%s", stdout)
	}

	code, stdout, stderr = runWith("policy", "eval", "--repo", "market-mini",
		"--worktree", violatingFixture(t), "--library", library, "--strict", "-o", "json")
	if code != exitOK {
		t.Errorf("--strict fails on waived violations: code %d, stderr %q\n%s", code, stderr, firstLines(stdout, 3))
	}
	var output struct {
		Waived []struct {
			Key        string `json:"key"`
			Constraint string `json:"constraint"`
			Site       string `json:"site"`
		} `json:"waived"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("eval output is not JSON: %v\n%s", err, firstLines(stdout, 3))
	}
	if len(output.Waived) != len(findings) {
		t.Errorf("the JSON output carries %d waivers, want %d", len(output.Waived), len(findings))
	}
}

func firstLines(text string, n int) string {
	lines := strings.SplitN(text, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func TestPolicyFindingsRefusesAnExceptionWithAnUnknownField(t *testing.T) {
	requireCodegraph(t)
	library := findingsLibrary(t)
	directory := filepath.Join(library, "exceptions")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	typo := "constraint: rfp-relay-only-guest-ssh\nkye: e8543cb5a9ecdcbc\nreason: a misspelt key\n"
	if err := os.WriteFile(filepath.Join(directory, "typo.yaml"), []byte(typo), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runWith("policy", "findings", "--repo", "market-mini",
		"--worktree", violatingFixture(t), "--library", library)
	if code != exitError {
		t.Fatalf("code %d, want %d: an exception with a misspelt key must not load", code, exitError)
	}
	if !strings.Contains(stderr, "kye") {
		t.Errorf("stderr does not name the unknown field: %q", stderr)
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

// `policy fix --apply` creates the SpecChange the spec flow realizes, instead
// of only printing the request (review 0006 N9). --dry-run prints it.
func TestPolicyFixApplyBuildsASpecChange(t *testing.T) {
	requireCodegraph(t)
	findings := runFindings(t, findingsLibrary(t))
	target := findings[0]
	hash := strings.Repeat("ab", 32)

	code, stdout, stderr := runWith("policy", "fix", target.Key,
		"--apply", "--dry-run", "--system-context", "market-mini",
		"--spec-hash", hash,
		"--repo", "market-mini", "--worktree", violatingFixture(t),
		"--library", findingsLibrary(t))
	if code != exitOK {
		t.Fatalf("fix --apply --dry-run: code %d, stderr %q", code, stderr)
	}
	var change struct {
		Metadata struct {
			Name        string            `json:"name"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			SystemContext string `json:"systemContext"`
			Direction     string `json:"direction"`
			ToSpecHash    string `json:"toSpecHash"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal([]byte(stdout), &change); err != nil {
		t.Fatalf("the change is not readable YAML: %v\n%s", err, stdout)
	}
	if change.Spec.SystemContext != "market-mini" || change.Spec.Direction != "SpecToCode" || change.Spec.ToSpecHash != hash {
		t.Errorf("the change does not target the context: %+v", change.Spec)
	}
	if !strings.Contains(change.Metadata.Name, "market-mini-s2c-") {
		t.Errorf("the change name does not name the context and the direction: %q", change.Metadata.Name)
	}
	if !strings.Contains(change.Metadata.Annotations["specs.publicdomainrelay.dev/fix-prompt"], target.Message) {
		t.Errorf("the change does not carry the prompt: %v", change.Metadata.Annotations)
	}
	if change.Metadata.Annotations["specs.publicdomainrelay.dev/fix-constraint"] != target.Constraint {
		t.Errorf("the change does not carry the constraint: %v", change.Metadata.Annotations)
	}
}

func TestPolicyFixApplyRefusesWithoutATarget(t *testing.T) {
	requireCodegraph(t)
	findings := runFindings(t, findingsLibrary(t))
	code, _, stderr := runWith("policy", "fix", findings[0].Key, "--apply", "--dry-run",
		"--repo", "market-mini", "--worktree", violatingFixture(t), "--library", findingsLibrary(t))
	if code != exitUsage {
		t.Fatalf("code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "--system-context") {
		t.Errorf("stderr does not ask for the context: %q", stderr)
	}
}

func TestPolicyWaiveRefusesAnExpiryItCannotParse(t *testing.T) {
	code, _, stderr := runWith("policy", "waive", "0000000000000000",
		"--reason", "accepted", "--expires", "not-a-date", "--repo", "market-mini")
	if code != exitUsage {
		t.Fatalf("code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "not an RFC3339 timestamp") {
		t.Errorf("stderr = %q", stderr)
	}
}
