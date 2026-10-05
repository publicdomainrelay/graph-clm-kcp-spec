package realize

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

// The realize gate is change-scoped (plan 0010 S6): a violation the base commit
// already carried is reported as inherited and does not block the change. The
// fixture is the violating market-mini, whose bidder already emits vm.onNetwork
// from the provisioning lifecycle and already reaches into the guest for the
// node id -- the shape the user decided not to fix, because the address may be
// a public IPv4 the client can judge.

func gateForBaselineTest(t *testing.T, dir, base string) (policy.Decision, policy.Report) {
	t.Helper()
	library, err := policyeval.Load(filepath.Join(fixture.Root(t), "examples", "policies", "market-mini"))
	if err != nil {
		t.Fatal(err)
	}
	decision, report, err := runPolicyGate(context.Background(), Options{
		Repository: &spec.Repository{
			ObjectMeta: metav1.ObjectMeta{Name: "market-mini", Namespace: "default"},
			Spec:       spec.RepositorySpec{Branch: "main"},
		},
		Change: "baseline-test",
		Branch: "main",
		Base:   base,
		Policy: &PolicyGateOptions{
			Library:   library,
			TestGlobs: []string{"test/**"},
		},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	return decision, report
}

func headCommit(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func inheritedConstraints(violations []policy.Violation) map[string]bool {
	out := map[string]bool{}
	for _, violation := range violations {
		out[violation.Constraint] = true
	}
	return out
}

func TestTheRealizeGateInheritsAViolationTheBaseAlreadyCarried(t *testing.T) {
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
	dir := fixture.Copy(t, filepath.Join("market-mini", "violating"))
	base := headCommit(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "NOTES.md"), []byte("# unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.Commit(t, dir, "docs: an unrelated note")

	decision, report := gateForBaselineTest(t, dir, base)
	if decision.Blocked {
		t.Fatalf("the change is blocked by a violation the base carried: %+v", decision.Denied)
	}
	if len(decision.Denied) != 0 {
		t.Fatalf("denied = %+v, want none", decision.Denied)
	}
	if len(decision.Inherited) == 0 {
		t.Fatalf("no inherited violation; the head report was %+v", report.Violations)
	}
	for _, violation := range decision.Inherited {
		if violation.Enforcement != policy.EnforcementWarn {
			t.Errorf("inherited violation %s is %s, want warn", violation.Constraint, violation.Enforcement)
		}
	}
	constraints := inheritedConstraints(decision.Inherited)
	for _, want := range []string{"rfp-host-reach-in", "rfp-relay-only-guest-ssh", "guest-report-driven-onnetwork"} {
		if !constraints[want] {
			t.Errorf("the inherited set does not carry %s: %v", want, constraints)
		}
	}
}

func TestTheRealizeGateDeniesAReachInTheChangeAdds(t *testing.T) {
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
	dir := fixture.Copy(t, filepath.Join("market-mini", "violating"))
	base := headCommit(t, dir)

	added := `import { createComputeProvider } from "@market-mini/compute-provider";

export async function probeGuest(providerId: string): Promise<string> {
  const provider = createComputeProvider(providerId);
  return await provider.getNodeId(providerId);
}
`
	if err := os.WriteFile(filepath.Join(dir, "hono-bidder", "probe.ts"), []byte(added), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.Commit(t, dir, "feat: probe the guest from the bidder")

	decision, report := gateForBaselineTest(t, dir, base)
	if !decision.Blocked {
		t.Fatalf("the new reach-in did not block the change; head report %+v", report.Violations)
	}
	found := false
	for _, violation := range decision.Denied {
		if violation.Constraint == "rfp-host-reach-in" && violation.Location != nil && violation.Location.File == "hono-bidder/probe.ts" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no rfp-host-reach-in denial at hono-bidder/probe.ts: %+v", decision.Denied)
	}
}
