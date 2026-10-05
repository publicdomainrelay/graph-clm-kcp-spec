package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	abcclm "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

const f4Repository = "f4-calc"

// F4.1: an apply that drops a requirement is refused unless the document names
// the removal, and the delta is printed by id before it lands.
func TestF4AnImplicitRequirementRemovalIsRefused(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", f4Repository)
	names := []string{"calc", "cmd-calc", f4Repository}
	repositories := []string{f4Repository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: f4Repository, Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: repoPath, Branch: "main", Verify: []string{"go", "test", "./..."}},
	})
	applyTyped(t, ctx, client, f4Context(t))

	specctl := buildSpecctl(t, root)
	rendered := runSpecctl(t, ctx, specctl, root, nil, "clm", "render", "--context", "calc")
	zone := f4ZoneWithout(t, rendered, "r.multiply")

	code, _, stderr := runSpecctlRaw(t, ctx, specctl, root, []byte(zone), "clm", "apply", "--context", "calc")
	if code == 0 {
		t.Fatalf("an apply that dropped r.multiply was accepted:\n%s", stderr)
	}
	if !strings.Contains(stderr, "r.multiply") || !strings.Contains(stderr, "removed:") {
		t.Errorf("the refusal does not name the id and the marker:\n%s", stderr)
	}
	if current := f4ReadContext(t, ctx, client, "calc"); len(current.Spec.Requirements) != 2 {
		t.Errorf("requirements = %+v, want the refused apply to have changed nothing", current.Spec.Requirements)
	}

	marked := strings.Replace(zone, abcclm.RemovalHint, "removed: [r.multiply]\n", 1)
	code, stdout, stderr := runSpecctlRaw(t, ctx, specctl, root, []byte(marked), "clm", "apply", "--context", "calc")
	if code != 0 {
		t.Fatalf("a marked removal was refused: %s\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "- r.multiply") {
		t.Errorf("the summary does not name the removal by id:\n%s", stderr)
	}
	current := f4ReadContext(t, ctx, client, "calc")
	if len(current.Spec.Requirements) != 1 || current.Spec.Requirements[0].ID != "r.add" {
		t.Errorf("requirements = %+v, want only r.add after the stated removal", current.Spec.Requirements)
	}
}

// F4.2: two quick applies to one context make two changes and land together,
// each with its own trailer, and neither delta is lost.
func TestF4TwoQuickAppliesMakeTwoChanges(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", f4Repository)
	names := []string{"calc", "cmd-calc", f4Repository}
	repositories := []string{f4Repository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: f4Repository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{"go", "test", "./..."},
			Agent:  &spec.AgentSpec{Kind: "scripted:" + f4Scenario(t)},
		},
	})
	applyTyped(t, ctx, client, f4Context(t))

	startPlan2bController(t, ctx, specdMaxAttempts, f4BatchWindow)

	waitFor(t, ctx, "the first ingest of calc", func() bool {
		return plan2bRealizedHash(t, ctx, client, "calc") != ""
	})
	base := headOf(t, repoPath)

	specctl := buildSpecctl(t, root)
	rendered := runSpecctl(t, ctx, specctl, root, nil, "clm", "render", "--context", "calc")
	first := f4ZoneWith(t, rendered, spec.Requirement{
		ID: "r.subtract", Level: spec.LevelMust, Text: "Subtract returns the difference.",
		CodeRefs: []string{"file:calc/extra.go"},
	}, spec.Interface{Name: "Subtract", Kind: "function", File: "calc/extra.go"})
	second := f4ZoneWith(t, first, spec.Requirement{
		ID: "r.divide", Level: spec.LevelMust, Text: "Divide returns the quotient.",
		CodeRefs: []string{"file:calc/extra.go"},
	}, spec.Interface{Name: "Divide", Kind: "function", File: "calc/extra.go"})

	if code, stdout, stderr := runSpecctlRaw(t, ctx, specctl, root, []byte(first), "clm", "apply", "--context", "calc"); code != 0 {
		t.Fatalf("the first apply failed: %s\n%s", stdout, stderr)
	}
	if code, stdout, stderr := runSpecctlRaw(t, ctx, specctl, root, []byte(second), "clm", "apply", "--context", "calc"); code != 0 {
		t.Fatalf("the second apply failed: %s\n%s", stdout, stderr)
	}

	waitFor(t, ctx, "two SpecToCode changes", func() bool {
		return len(changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)) == 2
	})
	waitFor(t, ctx, "both changes to succeed", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 2 && found[0].Status.Phase == specapi.PhaseSucceeded && found[1].Status.Phase == specapi.PhaseSucceeded
	})

	found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
	commit := found[0].Status.Commit
	if commit == "" || found[1].Status.Commit != commit {
		t.Fatalf("commits = %q and %q, want one commit naming both changes", commit, found[1].Status.Commit)
	}
	if count := gitOutput(t, repoPath, "rev-list", "--count", base+"..HEAD"); count != "1" {
		t.Errorf("the branch advanced by %s commit(s), want the one batch", count)
	}
	message := gitOutput(t, repoPath, "log", "-1", "--format=%B", commit)
	for _, change := range found {
		if !strings.Contains(message, "Spec-Change: "+change.Name) {
			t.Errorf("the commit message lacks the trailer of %s:\n%s", change.Name, message)
		}
	}
	current := f4ReadContext(t, ctx, client, "calc")
	ids := []string{}
	for _, requirement := range current.Spec.Requirements {
		ids = append(ids, requirement.ID)
	}
	for _, want := range []string{"r.add", "r.multiply", "r.subtract", "r.divide"} {
		if !contains(ids, want) {
			t.Errorf("requirements = %v, want %s: a delta was lost", ids, want)
		}
	}
	if headOf(t, repoPath) != commit {
		t.Errorf("HEAD = %s, want the batch commit %s", headOf(t, repoPath), commit)
	}
	assertNoSpecArtefacts(t, repoPath)
	runGoTest(t, repoPath)
}

const f4BatchWindow = 20 * time.Second

const specdMaxAttempts = 3

func f4Context(t *testing.T) *spec.SystemContext {
	t.Helper()
	systemContext := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: f4Repository,
			Upstream:   spec.RefSelf,
			Intent:     "Arithmetic on two integers.",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum of two integers.", CodeRefs: []string{"function:Add"}},
				{ID: "r.multiply", Level: spec.LevelMust, Text: "Multiply returns the product of two integers.", CodeRefs: []string{"function:Multiply"}},
			},
			Interfaces: []spec.Interface{
				{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go"},
				{Name: "Multiply", Kind: "function", Signature: "func Multiply(a, b int) int", File: "calc/calc.go"},
			},
			CodeRefs: []string{"file:calc/calc.go"},
		},
	}
	return systemContext
}

func f4Scenario(t *testing.T) string {
	t.Helper()
	scenario := `realize:
  calc:
    - write:
        path: calc/extra.go
        contents: |
          package calc

          func Subtract(a, b int) int { return a - b }

          func Divide(a, b int) int { return a / b }
    - write:
        path: calc/extra_test.go
        contents: |
          package calc

          import "testing"

          func TestSubtract(t *testing.T) {
          	if got := Subtract(5, 3); got != 2 {
          		t.Fatalf("Subtract(5, 3) = %d, want 2", got)
          	}
          }

          func TestDivide(t *testing.T) {
          	if got := Divide(6, 3); got != 2 {
          		t.Fatalf("Divide(6, 3) = %d, want 2", got)
          	}
          }
`
	target := filepath.Join(t.TempDir(), "f4.yaml")
	if err := os.WriteFile(target, []byte(scenario), 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

func f4ZoneWith(t *testing.T, zone string, requirement spec.Requirement, declared spec.Interface) string {
	t.Helper()
	parsed, err := abcclm.ParseModelZone(zone)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Requirements = append(parsed.Requirements, requirement)
	parsed.Interfaces = append(parsed.Interfaces, declared)
	rendered, err := abcclm.RenderModelZone("calc", f4Repository, parsed)
	if err != nil {
		t.Fatal(err)
	}
	return rendered
}

func f4ZoneWithout(t *testing.T, zone, id string) string {
	t.Helper()
	parsed, err := abcclm.ParseModelZone(zone)
	if err != nil {
		t.Fatal(err)
	}
	kept := []spec.Requirement{}
	for _, requirement := range parsed.Requirements {
		if requirement.ID != id {
			kept = append(kept, requirement)
		}
	}
	parsed.Requirements = kept
	rendered, err := abcclm.RenderModelZone("calc", f4Repository, parsed)
	if err != nil {
		t.Fatal(err)
	}
	return rendered
}

func f4ReadContext(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) *spec.SystemContext {
	t.Helper()
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	return typed.(*spec.SystemContext)
}

func runSpecctlRaw(t *testing.T, ctx context.Context, binary, root string, stdin []byte, args ...string) (int, string, string) {
	t.Helper()
	full := append(append([]string{}, args...),
		"--kubeconfig", e2eKubeconfig,
		"--workspace", e2eWorkspace,
		"--namespace", specapi.DefaultNamespace,
	)
	command := exec.CommandContext(ctx, binary, full...)
	command.Dir = root
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("specctl %s: %v", strings.Join(args, " "), err)
		}
	}
	return command.ProcessState.ExitCode(), stdout.String(), stderr.String()
}
