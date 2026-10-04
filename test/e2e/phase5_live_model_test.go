package e2e

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

func requireLiveModel(t *testing.T, tools ...string) {
	t.Helper()
	if os.Getenv("SPECD_REQUIRE_LIVE_MODEL") != "1" {
		t.Skip("live model test skipped; set SPECD_REQUIRE_LIVE_MODEL=1 to run it")
	}
	missing := []string{}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("SPECD_REQUIRE_LIVE_MODEL=1 but these tools are missing: %s", strings.Join(missing, ", "))
	}
}

func TestPhase5LiveModelSummarizesCalc(t *testing.T) {
	requireLiveModel(t, "deepseek-claude", "codegraph")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	repoPath := fixture.Copy(t, "calc")
	dbPath, err := codegraphsqlite.Ensure(ctx, repoPath, "")
	if err != nil {
		t.Fatalf("codegraph: %v", err)
	}
	database, err := codegraphsqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	facts, err := database.Facts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	partition, ok := partitionNamed(specsync.PartitionFacts(facts, "calc"), "calc")
	if !ok {
		t.Fatalf("the fixture has no calc partition: %+v", facts.Files)
	}
	observed := specsync.Observed(partition)
	if len(observed.Interfaces) != 2 {
		t.Fatalf("observed interfaces = %+v, want Add and Multiply", observed.Interfaces)
	}

	bundle := agent.ContextBundle{
		Context:    "calc",
		Repository: "calc",
		Spec:       spec.SystemContextSpec{Repository: "calc"},
		Observed:   observed,
		Budget:     8000,
	}
	runner := codegraphcli.Runner{Dir: repoPath}
	if text, err := runner.Context(ctx, "calc Add Multiply", 8); err == nil {
		bundle.CodeExcerpts = append(bundle.CodeExcerpts, agent.CodeExcerpt{Source: "codegraph context", Text: text})
	}
	if text, err := runner.Node(ctx, "Add"); err == nil {
		bundle.CodeExcerpts = append(bundle.CodeExcerpts, agent.CodeExcerpt{Source: "codegraph node Add", Text: text})
	}

	model := claudecli.New(claudecli.Options{Dir: repoPath, Timeout: 10 * time.Minute})
	draft, err := model.Summarize(ctx, bundle)
	if err != nil {
		t.Fatalf("the model did not answer with a readable spec: %v", err)
	}

	base := spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf}
	merged, result := agent.ValidateDraft("calc", base, draft)
	if !result.OK() {
		t.Fatalf("the model's draft does not validate: %v", result.Err())
	}
	if len(merged.Requirements) == 0 {
		t.Error("the model wrote no requirement")
	}
	names := []string{}
	for _, declared := range merged.Interfaces {
		names = append(names, declared.Name)
	}
	for _, want := range []string{"Add", "Multiply"} {
		if !namesContain(names, want) {
			t.Errorf("the model named %v, which does not include %s", names, want)
		}
	}
	if unresolved := specsync.UnresolvedCodeRefs(merged.Requirements, observed); len(unresolved) > 0 {
		t.Errorf("a requirement is anchored to nothing: %v", unresolved)
	}
	t.Logf("the model wrote %d requirement(s) and %d interface(s)", len(merged.Requirements), len(merged.Interfaces))
}

func partitionNamed(partitions []specsync.Partition, name string) (specsync.Partition, bool) {
	for _, partition := range partitions {
		if partition.Name == name {
			return partition, true
		}
	}
	return specsync.Partition{}, false
}

func namesContain(names []string, want string) bool {
	for _, name := range names {
		if strings.EqualFold(name, want) {
			return true
		}
	}
	return false
}
