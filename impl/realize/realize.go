package realize

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/bundle"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const (
	DefaultVerifyTimeout = 10 * time.Minute

	OutputTailBytes = 4000

	ChangedFilesLimit = 100

	WaitDelay = 5 * time.Second
)

type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)

	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)

	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

type Options struct {
	Cluster Cluster

	Namespace string

	Context string

	Change string

	Repository *spec.Repository

	Agent agent.Agent

	Delta spec.Delta

	Codegraph bundle.Codegraph

	Writer graph.Writer

	Budget int

	NodeLimit int

	ManagedBudget int

	Worktree string

	Branch string

	Base string

	Instruction string

	Tool string

	VerifyTimeout time.Duration
}

type Result struct {
	Context string

	Branch string

	Commit string

	VerifyExitCode int

	VerifyOutput string

	FilesTouched []string

	Agent agent.RealizeResult

	Landed bool

	SpecHash string
}

type VerifyError struct {
	Command []string

	ExitCode int

	Output string
}

func (e *VerifyError) Error() string {
	return fmt.Sprintf("realize: %s exited %d: %s", strings.Join(e.Command, " "), e.ExitCode, tail(e.Output))
}

func Run(ctx context.Context, options Options) (Result, error) {
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	result := Result{Context: options.Context, Branch: options.Branch}
	if options.Repository == nil {
		return result, fmt.Errorf("realize: no repository for %s", options.Context)
	}
	repoPath := options.Repository.WorkPath()
	branch := options.Repository.Spec.Branch
	if branch == "" {
		branch = "main"
	}

	systemContext, err := readContext(ctx, options.Cluster, namespace, options.Context)
	if err != nil {
		return result, err
	}
	realizedHash, err := spec.HashSystemContextSpec(systemContext.Spec)
	if err != nil {
		return result, err
	}
	result.SpecHash = realizedHash
	realizedSpec := systemContext.Spec

	built, err := bundle.Build(ctx, bundle.Options{
		Cluster:       options.Cluster,
		Namespace:     namespace,
		Context:       options.Context,
		Repository:    options.Repository,
		Writer:        options.Writer,
		Codegraph:     options.Codegraph,
		Budget:        options.Budget,
		NodeLimit:     options.NodeLimit,
		ManagedBudget: options.ManagedBudget,
	})
	if err != nil {
		return result, err
	}
	observed := built.Observed

	fromSpec := systemContext.Spec
	if systemContext.Status.RealizedSpec != nil {
		fromSpec = *systemContext.Status.RealizedSpec
	}

	if err := gitrepo.WorktreeAdd(ctx, repoPath, options.Worktree, options.Branch, options.Base); err != nil {
		return result, err
	}
	removed := false
	removeWorktree := func() error {
		if removed {
			return nil
		}
		removed = true
		return gitrepo.WorktreeRemove(ctx, repoPath, options.Worktree)
	}
	defer func() { _ = removeWorktree() }()

	agentResult, err := options.Agent.Realize(ctx, agent.RealizeRequest{
		Context:     options.Context,
		Change:      options.Change,
		Repository:  options.Repository.Name,
		Dir:         options.Worktree,
		Delta:       options.Delta,
		FromSpec:    fromSpec,
		ToSpec:      systemContext.Spec,
		Observed:    observed,
		ContextDoc:  built.ContextDoc,
		Verify:      options.Repository.Spec.Verify,
		Instruction: options.Instruction,
		Budget:      options.Budget,
	})
	result.Agent = agentResult
	if err != nil {
		return result, err
	}

	verifyCommand := options.Repository.Spec.Verify
	exitCode, output := verify(ctx, verifyCommand, options.Worktree, options.VerifyTimeout)
	result.VerifyExitCode = exitCode
	result.VerifyOutput = output
	if exitCode != 0 {
		return result, &VerifyError{Command: verifyCommand, ExitCode: exitCode, Output: output}
	}

	commit, err := gitrepo.CommitAll(ctx, options.Worktree, commitMessage(options.Context, options.Change, options.Repository.Name, options.Delta))
	if err != nil {
		return result, err
	}
	if commit != "" {
		files, err := gitrepo.ChangedFiles(ctx, options.Worktree, options.Base, commit)
		if err != nil {
			return result, err
		}
		result.FilesTouched = limit(files, ChangedFilesLimit)
		if err := removeWorktree(); err != nil {
			return result, err
		}
		if err := gitrepo.FastForward(ctx, repoPath, branch, commit); err != nil {
			return result, err
		}
		if err := gitrepo.DeleteBranch(ctx, repoPath, options.Branch); err != nil {
			return result, err
		}
		result.Commit = commit
		result.Landed = true
	}

	if err := settle(ctx, options, namespace, &realizedSpec); err != nil {
		return result, err
	}
	return result, nil
}

func Settle(ctx context.Context, options Options) error {
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	current, err := readContext(ctx, options.Cluster, namespace, options.Context)
	if err != nil {
		return err
	}
	adopted := current.Spec
	return settle(ctx, options, namespace, &adopted)
}

func settle(ctx context.Context, options Options, namespace string, adopted *spec.SystemContextSpec) error {
	repoPath := options.Repository.WorkPath()
	head, err := gitrepo.Head(ctx, repoPath)
	if err != nil {
		return err
	}
	_, err = ingest.Run(ctx, options.Cluster, ingest.Options{
		RepoPath:       repoPath,
		RepositoryName: options.Repository.Name,
		Namespace:      namespace,
		SpecPath:       repoPath,
		Tool:           options.Tool,
		Commit:         head,
		Adopt:          adopted,
		Writer:         options.Writer,
	})
	return err
}

func readContext(ctx context.Context, cluster Cluster, namespace, name string) (*spec.SystemContext, error) {
	object, err := cluster.Get(ctx, specapi.SystemContextGVR, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("realize: read systemcontext %s: %w", name, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return nil, fmt.Errorf("realize: %s is not a SystemContext", name)
	}
	return systemContext, nil
}

func verify(ctx context.Context, command []string, dir string, timeout time.Duration) (int, string) {
	if len(command) == 0 {
		return 0, ""
	}
	if timeout <= 0 {
		timeout = DefaultVerifyTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	process := exec.CommandContext(runCtx, command[0], command[1:]...)
	process.Dir = dir
	process.WaitDelay = WaitDelay
	output, err := process.CombinedOutput()
	if err == nil {
		return 0, tail(string(output))
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode(), tail(string(output))
	}
	return -1, tail(string(output) + "\n" + err.Error())
}

func commitMessage(context, change, repository string, delta spec.Delta) string {
	message := fmt.Sprintf("realize %s: %s\n\n", context, deltaSummary(delta))
	if change != "" {
		message += oabranch.SpecChangeTrailer + ": " + change + "\n"
	}
	if repository != "" {
		message += oabranch.OpenArchitectureTrailer + ": " + oabranch.Branch(repository) + "\n"
	}
	return message
}

func deltaSummary(change spec.Delta) string {
	counts := change.Count()
	parts := []string{}
	if counts.Added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", counts.Added))
	}
	if counts.Removed > 0 {
		parts = append(parts, fmt.Sprintf("-%d", counts.Removed))
	}
	if counts.Changed > 0 {
		parts = append(parts, fmt.Sprintf("~%d", counts.Changed))
	}
	if len(parts) == 0 {
		return "no delta"
	}
	return strings.Join(parts, " ")
}

func tail(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= OutputTailBytes {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-OutputTailBytes:]
}

func limit(values []string, maximum int) []string {
	if len(values) <= maximum {
		return values
	}
	return values[:maximum]
}
