package realize

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

// The fix path of plan 0010 U4: `policy fix` turns a violation into a
// SpecChange request whose prompt is what a realize hands to its agent. This
// drives the whole path -- the request, the scripted agent that acts on it,
// and the gate again -- and proves the violation is gone.
func TestAScriptedAgentRealizesThePromptOfAFixRequest(t *testing.T) {
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
	dir := fixture.Copy(t, filepath.Join("market-mini", "violating"))
	library, err := policyeval.Load(filepath.Join(fixture.Root(t), "examples", "policies", "market-mini"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{
		Repository: &spec.Repository{
			ObjectMeta: metav1.ObjectMeta{Name: "market-mini", Namespace: "default"},
			Spec:       spec.RepositorySpec{Branch: "main"},
		},
		Change: "fix-test",
		Branch: "main",
		Policy: &PolicyGateOptions{
			Library:   library,
			TestGlobs: []string{"test/**"},
		},
	}

	before, _, err := runPolicyGate(context.Background(), options, dir)
	if err != nil {
		t.Fatal(err)
	}
	violation, ok := firstViolation(before.Denied, "relay-only-ssh")
	if !ok {
		t.Fatalf("the violating fixture is not denied by relay-only-ssh: %+v", before.Denied)
	}

	request := policy.BuildFixRequest(library, violation)
	if !strings.Contains(request.Prompt, violation.Msg) {
		t.Fatalf("the request does not carry the violation message: %q", request.Prompt)
	}
	if request.Site != "lib/requester/mod.ts:17" || request.Repository != "market-mini" {
		t.Fatalf("the request does not name the site and the repository: %+v", request)
	}

	// The agent the spec flow would run: it reads the prompt, and its scripted
	// steps are the change that answers it -- a ProxyCommand on the ssh and no
	// direct dial.
	scenario := &scriptedagent.Scenario{Realize: map[string][]scriptedagent.Step{
		"requester": {{
			Write: &scriptedagent.Write{
				Path: "lib/requester/mod.ts",
				Contents: `import type { Contract } from "@market-mini/market-common";

export interface ContractResult {
  exitCode: number;
}

export async function runComputeContract(contract: Contract): Promise<ContractResult> {
  const command = new Deno.Command("ssh", {
    args: ["-o", "ProxyCommand=websocat --binary wss://relay/tunnel", ` + "`root@${contract.guestHost}`" + `, "true"],
  });
  const { code } = await command.output();
  return { exitCode: code };
}
`,
			},
		}},
	}}
	result, err := scriptedagent.New(scenario).Realize(context.Background(), agent.RealizeRequest{
		Dir:         dir,
		Instruction: request.Prompt,
		Members:     []agent.RealizeMember{{Context: "requester", Change: "fix-test"}},
		Attempt:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) == 0 {
		t.Fatal("the agent changed nothing")
	}

	after, _, err := runPolicyGate(context.Background(), options, dir)
	if err != nil {
		t.Fatal(err)
	}
	// The prompt names one site; the fix is that site. The test file keeps its
	// own direct dial, which is a different finding with a different key.
	for _, violation := range after.Denied {
		if violation.Constraint != "relay-only-ssh" && violation.Constraint != "rfp-relay-only-guest-ssh" {
			continue
		}
		if violation.Location != nil && violation.Location.File == "lib/requester/mod.ts" {
			t.Fatalf("%s still denies at the fixed site: %+v", violation.Constraint, violation)
		}
	}
	beforeCount := countConstraint(before.Denied, "relay-only-ssh")
	afterCount := countConstraint(after.Denied, "relay-only-ssh")
	if afterCount >= beforeCount {
		t.Fatalf("the fix removed no relay-only-ssh denial: %d before, %d after", beforeCount, afterCount)
	}
}

func countConstraint(violations []policy.Violation, constraint string) int {
	count := 0
	for _, violation := range violations {
		if violation.Constraint == constraint {
			count++
		}
	}
	return count
}

func firstViolation(violations []policy.Violation, constraint string) (policy.Violation, bool) {
	for _, violation := range violations {
		if violation.Constraint == constraint {
			return violation, true
		}
	}
	return policy.Violation{}, false
}
