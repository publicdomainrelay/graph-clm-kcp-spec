package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/agentfactory"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/piagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

func TestPhase8PiHostSummarizesCalc(t *testing.T) {
	requireLiveModel(t, "npx", "codegraph")

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

	factory, err := agentfactory.New(agentfactory.Options{
		Kind:    agentfactory.Pi,
		Timeout: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err := factory.Agent(nil, repoPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the pi host runs %s %v", piagent.DefaultCommand, piagent.DefaultArgs())
	draft, err := host.Summarize(ctx, bundle)
	if err != nil {
		t.Fatalf("the pi host did not answer with a readable spec: %v", err)
	}
	base := spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf}
	merged, result := agent.ValidateDraft("calc", base, spec.ObservedFacts{}, draft)
	if !result.OK() {
		t.Fatalf("the pi host's draft does not validate: %v", result.Err())
	}
	if len(merged.Requirements) == 0 {
		t.Error("the pi host wrote no requirement")
	}
	names := []string{}
	for _, declared := range merged.Interfaces {
		names = append(names, declared.Name)
	}
	for _, want := range []string{"Add", "Multiply"} {
		if !namesContain(names, want) {
			t.Errorf("the pi host named %v, which does not include %s", names, want)
		}
	}
	t.Logf("the pi host wrote %d requirement(s) and %d interface(s)", len(merged.Requirements), len(merged.Interfaces))
}
