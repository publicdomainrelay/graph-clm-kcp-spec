package claudecli

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func TestGeneratePromptCarriesTheRefusedAttempt(t *testing.T) {
	request := policy.GenerateRequest{
		Mode:         policy.GenerateModePolicy,
		Repository:   "example",
		Slug:         "relay-only",
		Prompt:       "the tests reach the guest only through the relay",
		Requirements: []string{"lib-requester#r.relay"},
		Attempt:      2,
		Failures:     []string{"suite: denied: unexpected number of violations", "mutation: the rule denies none of the derived cases: unrelayed-ssh"},
	}
	prompt := GeneratePrompt(request)
	for _, want := range []string{
		"## attempt 1 was refused",
		"mutation: the rule denies none of the derived cases: unrelayed-ssh",
		"lib-requester#r.relay",
		"templates/relay-only/src.rego",
		"tests/relay-only/inventory/allowed.yaml",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
	if strings.Contains(GeneratePrompt(policy.GenerateRequest{Slug: "x", Prompt: "y", Mode: policy.GenerateModePolicy}), "was refused") {
		t.Error("a first attempt is told it was refused")
	}
}

func TestGeneratePromptBindsAPack(t *testing.T) {
	request := policy.GenerateRequest{
		Mode:       policy.GenerateModeBind,
		Repository: "example",
		Slug:       "rfp-guest-isolation",
		Pack:       &policy.PackManifest{Name: "rfp-guest-isolation", Version: "v1", Roles: []string{"guest", "host"}, Vocabulary: []string{"channels/relay"}},
		Binding:    policy.Binding{Roles: map[string]policy.RoleBinding{"host": {Globs: []string{"hono-bidder/**"}}}},
		Model:      "kind: ArchitectureModel\n",
	}
	prompt := GeneratePrompt(request)
	for _, want := range []string{"the pack rfp-guest-isolation@v1", "requires these roles: guest, host", "Write `policies.yaml` in the working directory", "the ArchitectureModel of the head commit"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the binding prompt lacks %q", want)
		}
	}
	if strings.Contains(prompt, "templates/") {
		t.Error("the binding prompt asks for a rule")
	}
}
