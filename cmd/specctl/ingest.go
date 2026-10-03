package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"text/tabwriter"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/populate"
)

// runIngest is a thin wrapper around one Repository manifest: it applies the
// manifest and waits for the controller to reach Populated. The work itself is
// impl/populate, the same code path specd's Repository reconciler runs, so the
// CLI and the controller can never disagree about what ingesting means.
func runIngest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl ingest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", "", "path to the working tree to ingest")
	repoName := fs.String("repo-name", "", "Repository name; defaults to the directory name")
	partition := fs.String("partition", spec.PartitionDirectory, "partition mode: directory or package")
	summarizeNow := fs.Bool("summarize", false, "send the agent over every context whose intent is empty")
	agentKind := fs.String("agent", "", "agent to summarize with: scripted:<file> or claude")
	agentCommand := fs.String("agent-command", "", "model command to run (default deepseek-claude)")
	agentArgs := fs.String("agent-args", "", "model command arguments (default -p --output-format text)")
	wait := fs.Duration("wait", 10*time.Minute, "how long to wait for the repository to reach Populated")
	var include stringsFlag
	fs.Var(&include, "include", "glob of repository-relative paths to keep; repeatable")
	var exclude stringsFlag
	fs.Var(&exclude, "exclude", "glob of repository-relative paths to drop; repeatable")
	// Kept so an existing invocation still parses. The controller owns these
	// now: the graph is written where the index is written.
	_ = fs.String("codegraph", "", "codegraph command to run (the controller's --codegraph)")
	_ = fs.Bool("no-graph", false, "do not write the graph (the controller's --bolt-url)")
	_ = fs.Int("bundle-budget", 0, "token budget of the context bundle (the controller's --bundle-budget)")
	_ = fs.Int("bundle-nodes", 0, "codegraph node excerpts per bundle (the controller's)")
	_ = fs.Int("context-doc-budget", 0, "token budget of the context document (the controller's)")
	_ = fs.Duration("agent-timeout", 3*time.Minute, "how long one model call may take (the controller's)")
	options := addGlobals(fs)
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitUsage
	}
	if *repo == "" {
		fmt.Fprintln(stderr, "specctl ingest: --repo is required")
		return exitUsage
	}
	noteControllerFlags(fs, stderr)

	absolute, err := filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitError
	}
	name := *repoName
	if name == "" {
		name = ingest.SanitizeName(filepath.Base(absolute))
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitError
	}

	repository, err := buildRepository(ctx, client, options.namespace, name, absolute, *partition,
		include, exclude, *summarizeNow, *agentKind, *agentCommand, *agentArgs)
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitUsage
	}
	request := repository.Annotations[specapi.PopulateRequestAnnotation]
	object, err := kcpclient.Unstructured(repository)
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitError
	}
	if _, err := client.Apply(ctx, object); err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitError
	}

	waitCtx, cancel := context.WithTimeout(ctx, *wait)
	defer cancel()
	final, err := populate.WaitForPopulated(waitCtx, client, options.namespace, name, request, 250*time.Millisecond)
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		fmt.Fprintln(stderr, "specctl ingest: the Repository is applied; a running specd populates it")
		return exitError
	}
	contexts, err := contextsOf(ctx, client, options.namespace, name)
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitError
	}
	printPopulate(stdout, absolute, final, contexts)
	return exitOK
}

// buildRepository merges what the CLI owns into the Repository that is already
// there, so a re-run does not throw away fields the manifest holds for other
// parts of the loop (verify, branch, the realize agent).
func buildRepository(
	ctx context.Context,
	client *kcpclient.Client,
	namespace, name, path, partition string,
	include, exclude []string,
	summarize bool,
	agentKind, agentCommand, agentArgs string,
) (*spec.Repository, error) {
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	if existing, err := populate.Read(ctx, client, namespace, name); err == nil {
		repository = existing
	} else if !kcpclient.IsNotFound(err) {
		return nil, err
	}
	repository.Spec.Source = &spec.RepositorySource{Path: path}
	populateSpec := &spec.RepositoryPopulate{
		Partition: partition,
		Include:   include,
		Exclude:   exclude,
		Summarize: summarize,
	}
	if agentKind != "" || agentCommand != "" {
		populateSpec.Agent = &spec.AgentSpec{
			Kind:    agentKind,
			Command: agentCommand,
			Args:    splitArgs(agentArgs),
		}
	} else {
		populateSpec.Agent = repository.PopulateAgent()
	}
	repository.Spec.Populate = populateSpec
	if repository.Annotations == nil {
		repository.Annotations = map[string]string{}
	}
	// A fresh request token is what makes a re-run index again even though the
	// git HEAD has not moved; the controller records the one it answered.
	repository.Annotations[specapi.PopulateRequestAnnotation] = strconv.FormatInt(time.Now().UnixNano(), 10)
	repository.SetDefaults()
	if result := spec.ValidateRepository(repository); !result.OK() {
		return nil, result.Err()
	}
	return repository, nil
}

func contextsOf(ctx context.Context, client *kcpclient.Client, namespace, repository string) ([]spec.SystemContext, error) {
	listed, err := client.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	out := []spec.SystemContext{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		context, ok := typed.(*spec.SystemContext)
		if !ok || context.Spec.Repository != repository {
			continue
		}
		out = append(out, *context)
	}
	return out, nil
}

// noteControllerFlags tells a caller whose flags moved to the controller, so an
// established invocation is not silently half honoured.
func noteControllerFlags(fs *flag.FlagSet, stderr io.Writer) {
	controller := map[string]bool{
		"codegraph": true, "no-graph": true, "bundle-budget": true,
		"bundle-nodes": true, "context-doc-budget": true, "agent-timeout": true,
		"bolt-url": true, "bolt-user": true, "bolt-password": true,
		"bolt-password-file": true, "bolt-database": true, "bolt-backend": true,
	}
	fs.Visit(func(parsed *flag.Flag) {
		if controller[parsed.Name] {
			fmt.Fprintf(stderr, "specctl ingest: note: --%s configures the controller now; pass it to specd\n", parsed.Name)
		}
	})
}

func printPopulate(out io.Writer, path string, repository *spec.Repository, contexts []spec.SystemContext) {
	fmt.Fprintf(out, "repository\t%s\n", repository.Name)
	fmt.Fprintf(out, "path\t%s\n", path)
	fmt.Fprintf(out, "phase\t%s\n", repository.Status.Phase)
	if repository.Status.Contexts != nil {
		fmt.Fprintf(out, "contexts\t%d total, %d summarized, %d failed\n",
			repository.Status.Contexts.Total, repository.Status.Contexts.Summarized, repository.Status.Contexts.Failed)
	}
	fmt.Fprintln(out)
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "CONTEXT\tFILES\tIFACES\tREQS\tINTENT\tSPECVALID\tCODESYNCED\tDRIFTED")
	for index := range contexts {
		context := &contexts[index]
		intent := "no"
		if context.Spec.Intent != "" {
			intent = "yes"
		}
		fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%s\t%s\t%s\t%s\n",
			context.Name,
			len(context.Status.Observed.Files),
			len(context.Status.Observed.Interfaces),
			len(context.Spec.Requirements),
			intent,
			conditionStatusOf(context.Status.Conditions, specapi.ConditionSpecValid),
			conditionStatusOf(context.Status.Conditions, specapi.ConditionCodeSynced),
			conditionStatusOf(context.Status.Conditions, specapi.ConditionDrifted),
		)
	}
	table.Flush()
}
