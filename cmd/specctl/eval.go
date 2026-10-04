package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	eval "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphns"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/runlock"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
)

const (
	evalWorkspace    = "root:specs-eval"
	EnvEvalWorkspace = "SPECD_EVAL_WORKSPACE"
)

func defaultEvalWorkspace() string {
	if fromEnv := strings.TrimSpace(os.Getenv(EnvEvalWorkspace)); fromEnv != "" {
		return fromEnv
	}
	return evalWorkspace
}

func evalRunWorkspace() string {
	return "root:specs-eval-" + graphns.New("")
}

func ensureEvalWorkspace(ctx context.Context, kubeconfig, workspace string, stderr io.Writer) error {
	deployDir, err := session.ExtractDeploy()
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(kubeconfig)
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(workspace, "root:")
	command := exec.CommandContext(ctx, "bash", filepath.Join(deployDir, "install-specs.sh"))
	command.Env = append(os.Environ(),
		"ROOT="+filepath.Dir(absolute),
		"KUBECONFIG_PATH="+absolute,
		"SPECS_WORKSPACE="+name,
		"WORKSPACE_KUBECONFIG="+filepath.Join(filepath.Dir(absolute), name+".kubeconfig"),
	)
	command.Stdout = stderr
	command.Stderr = stderr
	return command.Run()
}

func deleteEvalWorkspace(kubeconfig, workspace string) {
	absolute, err := filepath.Abs(kubeconfig)
	if err != nil {
		return
	}
	name := strings.TrimPrefix(workspace, "root:")
	command := exec.Command("kubectl", "--kubeconfig", absolute, "delete", "workspace", name, "--wait=false")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	_ = command.Run()
}

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
	judgeCommand := fs.String("judge-command", "", "command that grades behavioural facts (default the model command)")
	judgeArgs := fs.String("judge-args", "", "arguments of the fact judge")
	suffice := fs.Bool("suffice", true, "measure spec sufficiency: strip implementation bodies, rebuild from the spec, run the original tests")
	baseline := fs.String("baseline", "", "a previous JSON report to compare against, for the not-discriminating check")
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
	liveLock := fs.String("live-lock", os.Getenv("SPECD_LIVE_LOCK"),
		"file the live lock is taken on, so two live runs that share it serialize instead of corrupting each other; empty takes no lock")
	options := addGlobals(fs)
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl eval: %v\n", err)
		return exitUsage
	}
	namedWorkspace := flagSet(fs, "workspace")
	if !namedWorkspace {
		options.workspace = defaultEvalWorkspace()
		namedWorkspace = os.Getenv(EnvEvalWorkspace) != ""
	}
	createdWorkspace := false
	if !namedWorkspace {
		options.workspace = evalRunWorkspace()
		createdWorkspace = true
	}
	if os.Getenv(graphns.Env) == "" {
		os.Setenv(graphns.Env, graphns.New("eval-"))
	}
	ctx := context.Background()
	if err := ensureEvalWorkspace(ctx, options.kubeconfig, options.workspace, stderr); err != nil {
		fmt.Fprintf(stderr, "specctl eval: %v\n", err)
		return exitError
	}
	if createdWorkspace && !*keep {
		defer deleteEvalWorkspace(options.kubeconfig, options.workspace)
	}

	if !flagSet(fs, "live-lock") && os.Getenv("SPECD_LIVE_LOCK") == "" {
		*liveLock = runlock.PathFor(filepath.Dir(options.kubeconfig), options.kubeconfig+"|"+options.workspace)
	}

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

	if *liveLock != "" {
		lock, err := runlock.Acquire(*liveLock, runlock.Options{Name: "specctl eval", Wait: stderr})
		if err != nil {
			fmt.Fprintf(stderr, "specctl eval: %v\n", err)
			return exitError
		}
		defer lock.Release()
	}
	fmt.Fprintf(stderr, "specctl eval: workspace %s, graph namespace %s, lock %s\n", options.workspace, os.Getenv(graphns.Env), *liveLock)

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
		SkipSuffice:    !*suffice,
		JudgeCommand:   *judgeCommand,
		JudgeArgs:      splitArgs(*judgeArgs),
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
	if *baseline != "" {
		comparison, err := compareWithBaseline(*baseline, report)
		if err != nil {
			fmt.Fprintf(stderr, "specctl eval: %v\n", err)
			return exitError
		}
		fmt.Fprint(stdout, comparison.Markdown())
		if *out != "" {
			if err := writeFile(comparePath(markdownPath(*out)), []byte(comparison.Markdown())); err != nil {
				fmt.Fprintf(stderr, "specctl eval: %v\n", err)
				return exitError
			}
		}
	}
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
	markdown, encoded := path, jsonPath(path)
	if strings.EqualFold(filepath.Ext(path), ".json") {
		markdown, encoded = markdownPath(path), path
	}
	if err := writeFile(markdown, []byte(report.Markdown())); err != nil {
		return err
	}
	body, err := report.JSON()
	if err != nil {
		return err
	}
	return writeFile(encoded, body)
}

func writeFile(path string, contents []byte) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, contents, 0o644)
}

func jsonPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".json"
}

func markdownPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".md"
}

func comparePath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + "-compare.md"
}

func compareWithBaseline(path string, report eval.Report) (eval.Comparison, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return eval.Comparison{}, fmt.Errorf("read the baseline %s: %w", path, err)
	}
	baseline, err := eval.ParseReport(contents)
	if err != nil {
		return eval.Comparison{}, err
	}
	return eval.Compare(baseline, report), nil
}

func countFailed(report eval.Report) int {
	failed := 0
	for _, scenario := range report.Scenarios {
		if scenario.Skipped {
			continue
		}
		if !scenario.Pass {
			failed++
		}
	}
	return failed
}
