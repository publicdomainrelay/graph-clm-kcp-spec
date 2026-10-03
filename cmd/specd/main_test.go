package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/agentfactory"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

func parse(t *testing.T, args ...string) (config, error) {
	t.Helper()
	config, _, err := parseConfig(args, io.Discard)
	return config, err
}

func TestParseConfigDefaultsAreTheControllerDefaults(t *testing.T) {
	config, err := parse(t)
	if err != nil {
		t.Fatal(err)
	}
	options, err := config.options(nil, logging.Discard())
	if err != nil {
		t.Fatal(err)
	}
	if options.Workspace != "root:specs" || options.Namespace != specapi.DefaultNamespace {
		t.Errorf("workspace/namespace = %q/%q", options.Workspace, options.Namespace)
	}
	if options.Watch != specd.WatchInformer || options.Resync != specd.DefaultResync || options.Workers != specd.DefaultWorkers {
		t.Errorf("watch/resync/workers = %q/%s/%d", options.Watch, options.Resync, options.Workers)
	}
	if options.Agent != "" {
		t.Errorf("agent = %q, want no agent by default", options.Agent)
	}
	if options.MaxAttempts != specd.DefaultMaxAttempts || options.RetryBackoff != specd.DefaultRetryBackoff {
		t.Errorf("maxAttempts/retryBackoff = %d/%s", options.MaxAttempts, options.RetryBackoff)
	}
}

func TestParseConfigWiresEveryFlagIntoTheController(t *testing.T) {
	config, err := parse(t,
		"--kubeconfig", "/tmp/kc",
		"--context", "ctx",
		"--workspace", "root:other",
		"--namespace", "team",
		"--qps", "7",
		"--burst", "9",
		"--watch", specd.WatchPoll,
		"--poll-interval", "3s",
		"--resync", "4s",
		"--workers", "2",
		"--codegraph", "/usr/local/bin/codegraph",
		"--agent", "claude",
		"--agent-command", "my-model",
		"--agent-args", "-p --verbose",
		"--agent-timeout", "90s",
		"--bundle-budget", "1234",
		"--bundle-nodes", "5",
		"--context-doc-budget", "321",
		"--max-attempts", "6",
		"--retry-backoff", "2m",
		"--log-level", "debug",
	)
	if err != nil {
		t.Fatal(err)
	}
	options, err := config.options(nil, logging.Discard())
	if err != nil {
		t.Fatal(err)
	}

	if options.Kubeconfig != "/tmp/kc" || options.Context != "ctx" || options.Workspace != "root:other" || options.Namespace != "team" {
		t.Errorf("kcp wiring = %q/%q/%q/%q", options.Kubeconfig, options.Context, options.Workspace, options.Namespace)
	}
	if options.QPS != 7 || options.Burst != 9 {
		t.Errorf("qps/burst = %v/%d", options.QPS, options.Burst)
	}
	if options.Watch != specd.WatchPoll || options.PollInterval != 3*time.Second || options.Resync != 4*time.Second || options.Workers != 2 {
		t.Errorf("watch wiring = %q/%s/%s/%d", options.Watch, options.PollInterval, options.Resync, options.Workers)
	}
	if options.Tool != "/usr/local/bin/codegraph" {
		t.Errorf("tool = %q", options.Tool)
	}
	if options.Agent != agentfactory.Claude || options.AgentCommand != "my-model" || options.AgentTimeout != 90*time.Second {
		t.Errorf("agent wiring = %q/%q/%s", options.Agent, options.AgentCommand, options.AgentTimeout)
	}
	if strings.Join(options.AgentArgs, " ") != "-p --verbose" {
		t.Errorf("agent args = %v", options.AgentArgs)
	}
	if options.Budget != 1234 || options.NodeLimit != 5 || options.ManagedBudget != 321 {
		t.Errorf("budget wiring = %d/%d/%d", options.Budget, options.NodeLimit, options.ManagedBudget)
	}
	if options.MaxAttempts != 6 || options.RetryBackoff != 2*time.Minute {
		t.Errorf("retry wiring = %d/%s", options.MaxAttempts, options.RetryBackoff)
	}
}

func TestParseConfigRejectsWhatTheControllerWouldRefuse(t *testing.T) {
	if _, err := parse(t, "--nope"); err == nil {
		t.Error("an unknown flag was accepted")
	}
	config, err := parse(t, "--log-level", "loud")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.options(nil, logging.Discard()); err == nil {
		t.Error("a bad log level was accepted")
	}
}

func TestParseConfigReadsTheBoltFlags(t *testing.T) {
	_, bolt, err := parseConfig([]string{"--bolt-url", "bolt://127.0.0.1:7687", "--bolt-password", "secret"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if bolt.URL != "bolt://127.0.0.1:7687" || bolt.Password != "secret" {
		t.Errorf("bolt = %+v", bolt)
	}
}

func TestRunWithABadLogLevelIsAUsageError(t *testing.T) {
	if code := run([]string{"--log-level", "loud"}, io.Discard, io.Discard); code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

func TestRunWithHelpSucceeds(t *testing.T) {
	if code := run([]string{"--help"}, io.Discard, io.Discard); code != exitOK {
		t.Errorf("exit = %d, want %d", code, exitOK)
	}
}

func TestCacheDirFlagBeatsTheEnvironment(t *testing.T) {
	t.Setenv("SPECD_CACHE_DIR", "/from/env")
	config, _, err := parseConfig(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if config.cacheDir != "/from/env" {
		t.Errorf("cache dir = %q, want the environment to fill the default", config.cacheDir)
	}
	config, _, err = parseConfig([]string{"--cache-dir", "/from/flag"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if config.cacheDir != "/from/flag" {
		t.Errorf("cache dir = %q, want the flag to beat the environment", config.cacheDir)
	}
	// A flag left at the default value is still not a flag the caller set.
	config, _, err = parseConfig([]string{"--cache-dir", specd.DefaultCacheDir}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if config.cacheDir != specd.DefaultCacheDir {
		t.Errorf("cache dir = %q", config.cacheDir)
	}
}
