package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	eval "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/eval"
)

// evalWorkspace is where an eval run keeps its state. It is deliberately not
// the workspace the live tests own: an eval run creates, changes and deletes
// whole repositories, and it must never disturb another suite's objects.
const evalWorkspace = "root:specs-eval"

func runEval(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fixtures := fs.String("fixtures", "fixtures", "directory of fixtures, one subdirectory per repository")
	scenarios := fs.String("scenarios", "", "glob over scenario names, empty for all")
	agent := fs.String("agent", "", "agent of both halves: empty for the scripted baseline, or claude, claude-mod or pi")
	summarizeAgent := fs.String("summarize-agent", "", "agent of the code -> spec half only")
	agentCommand := fs.String("agent-command", "", "model command to run (default deepseek-claude)")
	agentArgs := fs.String("agent-args", "", "model arguments (default -p --output-format text)")
	agentTimeout := fs.Duration("agent-timeout", 0, "how long one model call may take")
	clmMod := fs.String("clm-mod", os.Getenv("SPECD_CLM_MOD"), "cc-clm-mod folder the claude-mod kind loads")
	piExtension := fs.String("pi-extension", os.Getenv("SPECD_PI_EXTENSION"), "pi-hydradb-clm folder the pi kind loads")
	out := fs.String("out", "", "write the markdown report here; the JSON report lands beside it")
	workDir := fs.String("work-dir", "", "where the working trees go (default a temporary directory)")
	keep := fs.Bool("keep", false, "keep the working trees and the objects after the run")
	codeOnly := fs.Bool("code-only", false, "run the code -> spec half and the round trip only")
	roundTrip := fs.Bool("round-trip", true, "summarize every context twice more to measure round trip stability")
	timeout := fs.Duration("timeout", eval.DefaultTimeout, "how long one scenario may take")
	maxAttempts := fs.Int("max-attempts", eval.DefaultScenarioAttempts, "how many attempts one scenario's change gets")
	tool := fs.String("codegraph", "", "codegraph command to run")
	options := addGlobals(fs)
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl eval: %v\n", err)
		return exitUsage
	}
	if !flagSet(fs, "workspace") {
		options.workspace = evalWorkspace
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl eval: %v\n", err)
		return exitError
	}
	var writer graph.Writer
	if bolt.URL != "" {
		boltClient, err := bolt.Connect(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "specctl eval: %v\n", err)
			return exitError
		}
		defer boltClient.Close(context.Background())
		writer = boltClient
	}

	report, err := eval.Run(ctx, eval.Options{
		FixturesDir:    *fixtures,
		ScenarioGlob:   *scenarios,
		Agent:          *agent,
		SummarizeAgent: *summarizeAgent,
		AgentCommand:   *agentCommand,
		AgentArgs:      splitArgs(*agentArgs),
		AgentTimeout:   *agentTimeout,
		ClmMod:         *clmMod,
		PiExtension:    *piExtension,
		Kubeconfig:     options.kubeconfig,
		Context:        options.context,
		Workspace:      options.workspace,
		Namespace:      options.namespace,
		QPS:            float32(options.qps),
		Burst:          options.burst,
		Client:         client,
		WorkDir:        *workDir,
		Keep:           *keep,
		CodeOnly:       *codeOnly,
		NoRoundTrip:    !*roundTrip,
		Timeout:        *timeout,
		MaxAttempts:    *maxAttempts,
		Tool:           *tool,
		Writer:         writer,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl eval: %v\n", err)
		return exitError
	}
	fmt.Fprint(stdout, report.Markdown())
	if *out == "" {
		return exitOK
	}
	if err := writeReport(*out, report); err != nil {
		fmt.Fprintf(stderr, "specctl eval: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stderr, "specctl eval: wrote %s and %s\n", *out, jsonPath(*out))
	if failed := countFailed(report); failed > 0 {
		fmt.Fprintf(stderr, "specctl eval: %d scenario(s) did not pass\n", failed)
		return exitError
	}
	return exitOK
}

func writeReport(path string, report eval.Report) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, []byte(report.Markdown()), 0o644); err != nil {
		return err
	}
	encoded, err := report.JSON()
	if err != nil {
		return err
	}
	return os.WriteFile(jsonPath(path), encoded, 0o644)
}

func jsonPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".json"
}

func countFailed(report eval.Report) int {
	failed := 0
	for _, scenario := range report.Scenarios {
		if !scenario.Pass {
			failed++
		}
	}
	return failed
}
