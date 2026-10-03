package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/cmd/internal/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// config is every flag of the binary, separated from the controller so the
// wiring can be tested without a cluster.
type config struct {
	kubeconfig string

	contextName string

	workspace string

	namespace string

	qps float64

	burst int

	watch string

	pollInterval time.Duration

	resync time.Duration

	workers int

	tool string

	cacheDir string

	maxConcurrentSummaries int

	logLevel string

	agent string

	agentCommand string

	agentArgs string

	agentTimeout time.Duration

	budget int

	nodeLimit int

	managedBudget int

	maxAttempts int

	retryBackoff time.Duration
}

func parseConfig(args []string, stderr io.Writer) (config, *boltflags.Options, error) {
	config := config{}
	fs := flag.NewFlagSet("specd", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&config.kubeconfig, "kubeconfig", defaultKubeconfig(), "path to the kcp kubeconfig")
	fs.StringVar(&config.contextName, "context", "", "kubeconfig context to use")
	fs.StringVar(&config.workspace, "workspace", "root:specs", "logical cluster path, empty to use the kubeconfig as given")
	fs.StringVar(&config.namespace, "namespace", specapi.DefaultNamespace, "namespace to reconcile")
	fs.StringVar(&config.namespace, "n", specapi.DefaultNamespace, "namespace to reconcile")
	fs.Float64Var(&config.qps, "qps", 50, "requests per second against kcp")
	fs.IntVar(&config.burst, "burst", 100, "request burst against kcp")
	fs.StringVar(&config.watch, "watch", specd.WatchInformer, "how to watch the workspace: informer or poll")
	fs.DurationVar(&config.pollInterval, "poll-interval", specd.DefaultPollInterval, "list interval of the poll watch")
	fs.DurationVar(&config.resync, "resync", specd.DefaultResync, "how often a Repository is asked for its git HEAD")
	fs.IntVar(&config.workers, "workers", specd.DefaultWorkers, "concurrent reconciles")
	fs.StringVar(&config.tool, "codegraph", "", "codegraph command to run")
	fs.StringVar(&config.cacheDir, "cache-dir", specd.DefaultCacheDir, "where a Repository git source is cloned (env SPECD_CACHE_DIR)")
	fs.IntVar(&config.maxConcurrentSummaries, "max-concurrent-summaries", specd.DefaultMaxConcurrentSummaries, "how many contexts one populate summarizes at once")
	fs.StringVar(&config.logLevel, "log-level", "info", "debug, info, warn or error")
	fs.StringVar(&config.agent, "agent", "", "how CodeToSpec changes are worked off: claude, scripted:<file>, or empty to leave them for a human")
	fs.StringVar(&config.agentCommand, "agent-command", "", "model command to run (default deepseek-claude)")
	fs.StringVar(&config.agentArgs, "agent-args", "", "model command arguments (default -p --output-format text)")
	fs.DurationVar(&config.agentTimeout, "agent-timeout", specd.DefaultAgentTimeout, "how long one model call may take")
	fs.IntVar(&config.budget, "bundle-budget", 0, "token budget of the context bundle")
	fs.IntVar(&config.nodeLimit, "bundle-nodes", 0, "how many codegraph node excerpts a bundle carries")
	fs.IntVar(&config.managedBudget, "context-doc-budget", 0, "token budget of the context document managed zone")
	fs.IntVar(&config.maxAttempts, "max-attempts", specd.DefaultMaxAttempts, "how many attempts one drift episode gets")
	fs.DurationVar(&config.retryBackoff, "retry-backoff", specd.DefaultRetryBackoff, "wait before the second attempt at an episode")
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return config, bolt, err
	}
	if err := bolt.Resolve(fs); err != nil {
		return config, bolt, err
	}
	return config, bolt, nil
}

func (c config) options(writer graph.Writer, log *slog.Logger) (specd.Options, error) {
	level, err := parseLevel(c.logLevel)
	if err != nil {
		return specd.Options{}, err
	}
	if log == nil {
		log = logging.New(logging.Options{Service: "specd", Level: level})
	}
	return specd.Options{
		Kubeconfig:             c.kubeconfig,
		Context:                c.contextName,
		Workspace:              c.workspace,
		Namespace:              c.namespace,
		QPS:                    float32(c.qps),
		Burst:                  c.burst,
		Watch:                  c.watch,
		PollInterval:           c.pollInterval,
		Resync:                 c.resync,
		Workers:                c.workers,
		Tool:                   c.tool,
		CacheDir:               cacheDir(c.cacheDir),
		MaxConcurrentSummaries: c.maxConcurrentSummaries,
		Graph:                  writer,
		Agent:                  c.agent,
		AgentCommand:           c.agentCommand,
		AgentArgs:              strings.Fields(c.agentArgs),
		AgentTimeout:           c.agentTimeout,
		Budget:                 c.budget,
		NodeLimit:              c.nodeLimit,
		ManagedBudget:          c.managedBudget,
		MaxAttempts:            c.maxAttempts,
		RetryBackoff:           c.retryBackoff,
		Log:                    log,
	}, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	config, bolt, err := parseConfig(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	level, err := parseLevel(config.logLevel)
	if err != nil {
		fmt.Fprintf(stderr, "specd: %v\n", err)
		return exitUsage
	}
	logger := logging.New(logging.Options{Service: "specd", Writer: stderr, Level: level})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var writer graph.Writer
	if bolt.URL != "" {
		client, err := bolt.Connect(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "specd: %v\n", err)
			return exitError
		}
		defer client.Close(context.Background())
		writer = client
		logger.Info("the graph is rewritten after every ingest", "url", bolt.URL)
	}

	options, err := config.options(writer, logger)
	if err != nil {
		fmt.Fprintf(stderr, "specd: %v\n", err)
		return exitUsage
	}
	if options.Agent != "" {
		logger.Info("CodeToSpec changes are worked off by an agent", "agent", options.Agent, "maxAttempts", options.MaxAttempts)
	}

	controller, err := specd.New(options)
	if err != nil {
		fmt.Fprintf(stderr, "specd: %v\n", err)
		return exitError
	}

	logger.Info("watching the workspace",
		"workspace", config.workspace, "namespace", config.namespace, "watch", config.watch, "resync", config.resync.String())
	if err := controller.Run(ctx); err != nil {
		logger.Error("the controller stopped", "err", err)
		fmt.Fprintf(stderr, "specd: %v\n", err)
		return exitError
	}
	logger.Info("stopped")
	fmt.Fprintln(stdout, "specd stopped")
	return exitOK
}

// cacheDir reads the cache directory from the flag, or from SPECD_CACHE_DIR
// when the flag was left at its default, so a deployment can point every
// Repository checkout at one volume.
func cacheDir(fromFlag string) string {
	if fromFlag != specd.DefaultCacheDir {
		return fromFlag
	}
	if fromEnv := os.Getenv("SPECD_CACHE_DIR"); fromEnv != "" {
		return fromEnv
	}
	return fromFlag
}

func defaultKubeconfig() string {
	if fromEnv := os.Getenv("SPECD_KUBECONFIG"); fromEnv != "" {
		return fromEnv
	}
	return ".kcp-specd/admin.kubeconfig"
}

func parseLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, errors.New("--log-level is debug, info, warn or error")
}
