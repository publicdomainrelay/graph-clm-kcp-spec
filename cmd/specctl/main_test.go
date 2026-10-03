package main

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
)

func runWith(args ...string) (int, string, string) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := run(args, stdout, stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunDispatch(t *testing.T) {
	t.Setenv("SPECD_BOLT_URL", "")

	code, _, stderr := runWith()
	if code != exitUsage || !strings.Contains(stderr, "usage:") {
		t.Errorf("no arguments: code %d, stderr %q", code, stderr)
	}

	code, _, stderr = runWith("explode")
	if code != exitUsage || !strings.Contains(stderr, "unknown command") {
		t.Errorf("unknown command: code %d, stderr %q", code, stderr)
	}

	code, stdout, _ := runWith("help")
	if code != exitOK || !strings.Contains(stdout, "specctl ingest") {
		t.Errorf("help: code %d, stdout %q", code, stdout)
	}

	code, stdout, _ = runWith("--help")
	if code != exitOK || !strings.Contains(stdout, "specctl graph") {
		t.Errorf("--help: code %d, stdout %q", code, stdout)
	}
}

func TestRunApplyNeedsAFile(t *testing.T) {
	code, _, stderr := runWith("apply")
	if code != exitUsage || !strings.Contains(stderr, "-f is required") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestRunDeleteNeedsKindAndName(t *testing.T) {
	code, _, stderr := runWith("delete", "systemcontext")
	if code != exitUsage || !strings.Contains(stderr, "kind and a name") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestRunGetRejectsAnUnknownKind(t *testing.T) {
	code, _, stderr := runWith("get", "bananas")
	if code != exitUsage || !strings.Contains(stderr, "unknown kind") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestRunIngestNeedsARepo(t *testing.T) {
	code, _, stderr := runWith("ingest")
	if code != exitUsage || !strings.Contains(stderr, "--repo is required") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestRunGraphSubcommands(t *testing.T) {
	t.Setenv("SPECD_BOLT_URL", "")

	code, _, stderr := runWith("graph")
	if code != exitUsage || !strings.Contains(stderr, "neighbors or rebuild") {
		t.Errorf("graph alone: code %d, stderr %q", code, stderr)
	}

	code, _, stderr = runWith("graph", "wat")
	if code != exitUsage || !strings.Contains(stderr, "unknown subcommand") {
		t.Errorf("graph wat: code %d, stderr %q", code, stderr)
	}

	code, _, stderr = runWith("graph", "neighbors", "calc")
	if code != exitUsage || !strings.Contains(stderr, "--bolt-url") {
		t.Errorf("neighbors without a bolt url: code %d, stderr %q", code, stderr)
	}

	code, _, stderr = runWith("graph", "neighbors")
	if code != exitUsage || !strings.Contains(stderr, "one context name") {
		t.Errorf("neighbors without a name: code %d, stderr %q", code, stderr)
	}

	code, _, stderr = runWith("graph", "rebuild")
	if code != exitUsage || !strings.Contains(stderr, "--bolt-url") {
		t.Errorf("rebuild without a bolt url: code %d, stderr %q", code, stderr)
	}
}

func TestRunImportArchNeedsAPath(t *testing.T) {
	code, _, stderr := runWith("import-arch")
	if code != exitUsage || !strings.Contains(stderr, "arch.yaml path is required") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestRunExportRejectsAnUnknownFormat(t *testing.T) {
	code, _, stderr := runWith("export", "--format", "json")
	if code != exitUsage || !strings.Contains(stderr, "only arch is supported") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestRunExportHelpMentionsTheCommands(t *testing.T) {
	code, stdout, _ := runWith("help")
	if code != exitOK || !strings.Contains(stdout, "specctl import-arch") || !strings.Contains(stdout, "specctl export") {
		t.Errorf("code %d, stdout %q", code, stdout)
	}
}

func TestResolveContextName(t *testing.T) {
	for value, want := range map[string]string{
		"sc.deno-kcp":     "sc-deno-kcp",
		"sc.kind.denopod": "sc-kind-denopod",
		"sc-deno-kcp":     "sc-deno-kcp",
		"calc":            "calc",
	} {
		if got := resolveContextName(value); got != want {
			t.Errorf("resolveContextName(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestParseInterspersedKeepsPositionals(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	output := fs.String("o", "table", "output")
	positional, err := parseInterspersed(fs, []string{"systemcontext", "-o", "yaml", "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(positional) != 2 || positional[0] != "systemcontext" || positional[1] != "calc" {
		t.Fatalf("positional = %v", positional)
	}
	if *output != "yaml" {
		t.Fatalf("output = %q", *output)
	}
}

func TestGlobalsDefaultsAndEnv(t *testing.T) {
	t.Setenv("SPECD_KUBECONFIG", "/tmp/custom.kubeconfig")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	options := addGlobals(fs)
	if options.kubeconfig != "/tmp/custom.kubeconfig" {
		t.Errorf("kubeconfig = %q", options.kubeconfig)
	}
	if options.workspace != "root:specs" {
		t.Errorf("workspace = %q", options.workspace)
	}
	if options.namespace != specapi.DefaultNamespace {
		t.Errorf("namespace = %q", options.namespace)
	}
}

func TestBoltOptionsReadTheEnvironment(t *testing.T) {
	t.Setenv("SPECD_BOLT_URL", "bolt://example:7687")
	t.Setenv("SPECD_BOLT_USER", "root")
	t.Setenv("SPECD_BOLT_PASSWORD", "secret")
	t.Setenv("SPECD_BOLT_PASSWORD_FILE", "/tmp/token")
	t.Setenv("SPECD_BOLT_DATABASE", "clm")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	options := boltflags.Add(fs)
	if err := fs.Parse([]string{"--bolt-url", "bolt://other:7687"}); err != nil {
		t.Fatal(err)
	}
	if err := options.Resolve(fs); err != nil {
		t.Fatal(err)
	}
	if options.URL != "bolt://other:7687" {
		t.Errorf("a flag must beat the environment: %q", options.URL)
	}
	if options.User != "root" || options.Password != "secret" ||
		options.PasswordFile != "/tmp/token" || options.Database != "clm" {
		t.Errorf("bolt options = %+v", options)
	}
}

func TestDefaultKubeconfigFallsBackToTheRepositoryState(t *testing.T) {
	t.Setenv("SPECD_KUBECONFIG", "")
	if got := defaultKubeconfig(); got != ".kcp-specd/admin.kubeconfig" {
		t.Errorf("kubeconfig = %q", got)
	}
}

// `clm render --context <name>` names a SystemContext, and the global --context
// names a kubeconfig context: the subcommand must not register the same flag
// twice, which would panic, and must not read the wrong one.
func TestCLMContextNamesTheSystemContext(t *testing.T) {
	contextName := ""
	options, err := newCLMFlags([]string{"--context", "calc", "--workspace", "root:tenant"}, io.Discard, "render",
		func(fs *flag.FlagSet) {
			fs.StringVar(&contextName, "context", "", "SystemContext to render")
		})
	if err != nil {
		t.Fatal(err)
	}
	if contextName != "calc" {
		t.Errorf("--context = %q, want the SystemContext name", contextName)
	}
	if options.kube.workspace != "root:tenant" {
		t.Errorf("workspace = %q", options.kube.workspace)
	}
	if options.kube.context != "" {
		t.Errorf("the kubeconfig context must not be read from --context: %q", options.kube.context)
	}
}

func TestCLMWithoutASubcommandIsUsage(t *testing.T) {
	if code := run([]string{"clm"}, io.Discard, io.Discard); code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

// TestSyncValidatesItsArguments: the mirror refuses a direction or a preference
// it does not know, and needs a tree, before it touches kcp or the disk.
func TestSyncValidatesItsArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
	}{
		{name: "no repo", args: []string{"sync"}, code: exitUsage},
		{name: "unknown direction", args: []string{"sync", "--repo", ".", "--direction", "sideways"}, code: exitUsage},
		{name: "unknown preference", args: []string{"sync", "--repo", ".", "--prefer", "mine"}, code: exitUsage},
		{name: "missing tree", args: []string{"sync", "--repo", "/definitely/not/here"}, code: exitError},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := run(testCase.args, io.Discard, io.Discard); got != testCase.code {
				t.Errorf("exit = %d, want %d", got, testCase.code)
			}
		})
	}
}
