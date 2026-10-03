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
	"syscall"

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

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specd", flag.ContinueOnError)
	fs.SetOutput(stderr)
	kubeconfig := fs.String("kubeconfig", defaultKubeconfig(), "path to the kcp kubeconfig")
	contextName := fs.String("context", "", "kubeconfig context to use")
	workspace := fs.String("workspace", "root:specs", "logical cluster path, empty to use the kubeconfig as given")
	namespace := fs.String("namespace", specapi.DefaultNamespace, "namespace to reconcile")
	fs.StringVar(namespace, "n", specapi.DefaultNamespace, "namespace to reconcile")
	qps := fs.Float64("qps", 50, "requests per second against kcp")
	burst := fs.Int("burst", 100, "request burst against kcp")
	watch := fs.String("watch", specd.WatchInformer, "how to watch the workspace: informer or poll")
	pollInterval := fs.Duration("poll-interval", specd.DefaultPollInterval, "list interval of the poll watch")
	resync := fs.Duration("resync", specd.DefaultResync, "how often a Repository is asked for its git HEAD")
	workers := fs.Int("workers", specd.DefaultWorkers, "concurrent reconciles")
	tool := fs.String("codegraph", "", "codegraph command to run")
	logLevel := fs.String("log-level", "info", "debug, info, warn or error")
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	level, err := parseLevel(*logLevel)
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

	controller, err := specd.New(specd.Options{
		Kubeconfig:   *kubeconfig,
		Context:      *contextName,
		Workspace:    *workspace,
		Namespace:    *namespace,
		QPS:          float32(*qps),
		Burst:        *burst,
		Watch:        *watch,
		PollInterval: *pollInterval,
		Resync:       *resync,
		Workers:      *workers,
		Tool:         *tool,
		Graph:        writer,
		Log:          logger,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specd: %v\n", err)
		return exitError
	}

	logger.Info("watching the workspace",
		"workspace", *workspace, "namespace", *namespace, "watch", *watch, "resync", resync.String())
	if err := controller.Run(ctx); err != nil {
		logger.Error("the controller stopped", "err", err)
		fmt.Fprintf(stderr, "specd: %v\n", err)
		return exitError
	}
	logger.Info("stopped")
	fmt.Fprintln(stdout, "specd stopped")
	return exitOK
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
