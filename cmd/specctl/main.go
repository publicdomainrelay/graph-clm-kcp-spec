package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	positional := []string{}
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
}

type stringsFlag []string

func (s *stringsFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringsFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type globals struct {
	kubeconfig string
	context    string
	workspace  string
	namespace  string
	qps        float64
	burst      int
}

// The client-go default of 5 requests per second makes a bulk import of a few
// hundred objects take minutes; specctl raises it, and the flags let a caller
// back off again.
func addGlobals(fs *flag.FlagSet) *globals {
	options := &globals{}
	fs.StringVar(&options.kubeconfig, "kubeconfig", defaultKubeconfig(), "path to the kcp kubeconfig")
	fs.StringVar(&options.context, "context", "", "kubeconfig context to use")
	fs.StringVar(&options.workspace, "workspace", "root:specs", "logical cluster path, empty to use the kubeconfig as given")
	fs.StringVar(&options.namespace, "namespace", specapi.DefaultNamespace, "namespace to work in")
	fs.StringVar(&options.namespace, "n", specapi.DefaultNamespace, "namespace to work in")
	fs.Float64Var(&options.qps, "qps", 50, "requests per second against kcp")
	fs.IntVar(&options.burst, "burst", 100, "request burst against kcp")
	return options
}

// splitArgs reads one flag's worth of model arguments the way a shell would
// not: quoted whole, then split on whitespace, so `--agent-args "-p --verbose"`
// reaches the model as two arguments.
func splitArgs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Fields(value)
}

func defaultKubeconfig() string {
	if fromEnv := os.Getenv("SPECD_KUBECONFIG"); fromEnv != "" {
		return fromEnv
	}
	return ".kcp-specd/admin.kubeconfig"
}

func (g *globals) client() (*kcpclient.Client, error) {
	return kcpclient.New(kcpclient.Options{
		Kubeconfig: g.kubeconfig,
		Context:    g.context,
		Workspace:  g.workspace,
		Namespace:  g.namespace,
		QPS:        float32(g.qps),
		Burst:      g.burst,
	})
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	command, rest := args[0], args[1:]
	switch command {
	case "apply":
		return runApply(rest, stdout, stderr)
	case "get":
		return runGet(rest, stdout, stderr)
	case "delete":
		return runDelete(rest, stdout, stderr)
	case "ingest":
		return runIngest(rest, stdout, stderr)
	case "graph":
		return runGraph(rest, stdout, stderr)
	case "import-arch":
		return runImportArch(rest, stdout, stderr)
	case "export":
		return runExport(rest, stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return exitOK
	}
	fmt.Fprintf(stderr, "specctl: unknown command %q\n", command)
	usage(stderr)
	return exitUsage
}

func runApply(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	files := stringsFlag{}
	fs.Var(&files, "f", "manifest file to apply, - for stdin; repeatable")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if len(files) == 0 {
		fmt.Fprintln(stderr, "specctl apply: -f is required")
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl apply: %v\n", err)
		return exitError
	}

	failed := false
	for _, file := range files {
		data, err := readFile(file)
		if err != nil {
			fmt.Fprintf(stderr, "specctl apply: %v\n", err)
			return exitError
		}
		objects, err := kcpclient.Decode(data)
		if err != nil {
			fmt.Fprintf(stderr, "specctl apply: %s: %v\n", file, err)
			return exitError
		}
		for _, object := range objects {
			if !applyOne(ctx, client, object, stdout, stderr) {
				failed = true
			}
		}
	}
	if failed {
		return exitError
	}
	return exitOK
}

func applyOne(ctx context.Context, client *kcpclient.Client, object *unstructured.Unstructured, stdout, stderr io.Writer) bool {
	typed, err := kcpclient.Typed(object)
	if err != nil {
		fmt.Fprintf(stderr, "specctl apply: %v\n", err)
		return false
	}
	spec.SetDefaults(typed)
	if result := spec.ValidateAny(typed); !result.OK() {
		fmt.Fprintf(stderr, "specctl apply: %s/%s is invalid: %v\n", object.GetKind(), object.GetName(), result.Err())
		return false
	}

	stamped, err := kcpclient.Unstructured(typed)
	if err != nil {
		fmt.Fprintf(stderr, "specctl apply: %v\n", err)
		return false
	}
	applied, err := client.Apply(ctx, stamped)
	if err != nil {
		fmt.Fprintf(stderr, "specctl apply: %v\n", err)
		return false
	}
	fmt.Fprintf(stdout, "%s/%s applied\n", strings.ToLower(applied.GetKind()), applied.GetName())
	return true
}

func runGet(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "table", "output format: table, yaml, json or name")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) == 0 {
		fmt.Fprintln(stderr, "specctl get: a kind is required")
		return exitUsage
	}
	kind, err := specapi.KindForArg(positional[0])
	if err != nil {
		fmt.Fprintf(stderr, "specctl get: %v\n", err)
		return exitUsage
	}
	gvr, err := specapi.GVRForKind(kind)
	if err != nil {
		fmt.Fprintf(stderr, "specctl get: %v\n", err)
		return exitError
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl get: %v\n", err)
		return exitError
	}

	items := []unstructured.Unstructured{}
	if len(positional) > 1 {
		found, err := client.Get(ctx, gvr, options.namespace, positional[1])
		if err != nil {
			fmt.Fprintf(stderr, "specctl get: %v\n", err)
			return exitError
		}
		items = append(items, *found)
	} else {
		listed, err := client.List(ctx, gvr, options.namespace)
		if err != nil {
			fmt.Fprintf(stderr, "specctl get: %v\n", err)
			return exitError
		}
		items = append(items, listed.Items...)
	}

	if err := printObjects(stdout, items, *output); err != nil {
		fmt.Fprintf(stderr, "specctl get: %v\n", err)
		return exitError
	}
	return exitOK
}

func runDelete(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 2 {
		fmt.Fprintln(stderr, "specctl delete: a kind and a name are required")
		return exitUsage
	}
	kind, err := specapi.KindForArg(positional[0])
	if err != nil {
		fmt.Fprintf(stderr, "specctl delete: %v\n", err)
		return exitUsage
	}
	gvr, err := specapi.GVRForKind(kind)
	if err != nil {
		fmt.Fprintf(stderr, "specctl delete: %v\n", err)
		return exitError
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl delete: %v\n", err)
		return exitError
	}
	name := positional[1]
	if err := client.Delete(ctx, gvr, options.namespace, name); err != nil {
		fmt.Fprintf(stderr, "specctl delete: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "%s/%s deleted\n", strings.ToLower(kind), name)
	return exitOK
}

func readFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func usage(out io.Writer) {
	fmt.Fprint(out, `specctl manages the spec custom resources in a kcp workspace.

usage:
  specctl apply -f <file> [-f <file>...] [--workspace root:specs]
  specctl get <kind> [name] [-o table|yaml|json|name]
  specctl delete <kind> <name>
  specctl ingest --repo <path> [--repo-name <name>] [--bolt-url <url>]
  specctl ingest --repo <path> --summarize --agent scripted:<file>|claude
  specctl graph neighbors <context> [--bolt-url <url>]
  specctl graph rebuild [--bolt-url <url>]
  specctl import-arch <arch.yaml> [--repository <name>] [--bolt-url <url>]
  specctl export --format arch [--repository <name>] [-o <file>]

kinds:
  repository, systemcontext, specchange

global flags:
  --kubeconfig <path>   kcp kubeconfig (default $SPECD_KUBECONFIG or .kcp-specd/admin.kubeconfig)
  --context <name>      kubeconfig context
  --workspace <path>    logical cluster path (default root:specs, empty to use the kubeconfig as given)
  --namespace, -n <ns>  namespace (default default)
  --qps <n>             requests per second against kcp (default 50)
  --burst <n>           request burst against kcp (default 100)

summarize flags (specctl ingest --summarize):
  --agent <kind>          scripted:<file> or claude
  --agent-command <cmd>   model command (default deepseek-claude)
  --agent-args <args>     model arguments (default -p --output-format text)
  --agent-timeout <dur>   how long one model call may take (default 3m)
  --bundle-budget <n>     token budget of the context bundle (default 8000)
  --bundle-nodes <n>      codegraph node excerpts per bundle (default 3)
  --context-doc-budget <n>  token budget of the context document (default 1500)

graph flags:
  --bolt-backend <name>      arcadedb (default) or hydradb; it fills the unset options
  --bolt-url <url>           bolt endpoint (default $SPECD_BOLT_URL or the backend's, empty skips the graph)
  --bolt-user <user>         bolt user (default $SPECD_BOLT_USER or the backend's)
  --bolt-password <password> bolt password (default $SPECD_BOLT_PASSWORD or the backend's)
  --bolt-password-file <path> read the bolt password from a file (default $SPECD_BOLT_PASSWORD_FILE or the backend's)
  --bolt-database <name>     bolt database (default $SPECD_BOLT_DATABASE or the backend's, needed by ArcadeDB)
`)
}
