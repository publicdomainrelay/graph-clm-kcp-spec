package realize

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
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
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
)

const (
	DefaultVerifyTimeout = 10 * time.Minute

	DefaultAcceptanceTimeout = 10 * time.Minute

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

type Member struct {
	Context string

	Change string

	Delta spec.Delta
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

	AcceptanceTimeout time.Duration

	WorktreeRoot string

	Members []Member
}

func (o Options) batch() []Member {
	if len(o.Members) > 0 {
		return o.Members
	}
	return []Member{{Context: o.Context, Change: o.Change, Delta: o.Delta}}
}

type Result struct {
	Context string

	Branch string

	Commit string

	VerifyExitCode int

	VerifyOutput string

	FilesTouched []string

	Acceptance []spec.AcceptanceResult

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

type AcceptanceError struct {
	Result spec.AcceptanceResult

	Gated bool
}

func (e *AcceptanceError) Error() string {
	kind := "report"
	if e.Gated {
		kind = "gate"
	}
	return fmt.Sprintf("realize: acceptance %s (%s) exited %d: %s", e.Result.Name, kind, e.Result.ExitCode, tail(e.Result.OutputTail))
}

type target struct {
	member Member

	toSpec spec.SystemContextSpec

	fromSpec spec.SystemContextSpec

	observed spec.ObservedFacts

	contextDoc string
}

func Run(ctx context.Context, options Options) (Result, error) {
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	members := options.batch()
	result := Result{Context: members[0].Context, Branch: options.Branch}
	if options.Repository == nil {
		return result, fmt.Errorf("realize: no repository for %s", members[0].Context)
	}
	repoPath := options.Repository.WorkPath()
	branch := options.Repository.Spec.Branch
	if branch == "" {
		branch = "main"
	}

	targets := make([]target, 0, len(members))
	for _, member := range members {
		systemContext, err := readContext(ctx, options.Cluster, namespace, member.Context)
		if err != nil {
			return result, err
		}
		built, err := bundle.Build(ctx, bundle.Options{
			Cluster:       options.Cluster,
			Namespace:     namespace,
			Context:       member.Context,
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
		fromSpec := systemContext.Spec
		if systemContext.Status.RealizedSpec != nil {
			fromSpec = *systemContext.Status.RealizedSpec
		}
		targets = append(targets, target{
			member:     member,
			toSpec:     systemContext.Spec,
			fromSpec:   fromSpec,
			observed:   built.Observed,
			contextDoc: built.ContextDoc,
		})
	}
	realizedHash, err := spec.HashSystemContextSpec(targets[0].toSpec)
	if err != nil {
		return result, err
	}
	result.SpecHash = realizedHash

	if err := gitrepo.WorktreeAdd(ctx, repoPath, options.Worktree, options.Branch, options.Base); err != nil {
		return result, err
	}
	removed := false
	removeWorktree := func() error {
		if removed {
			return nil
		}
		removed = true
		err := gitrepo.WorktreeRemove(ctx, repoPath, options.Worktree)
		if options.WorktreeRoot != "" {
			_ = os.RemoveAll(options.WorktreeRoot)
		}
		return err
	}
	defer func() { _ = removeWorktree() }()

	request := agent.RealizeRequest{
		Context:     members[0].Context,
		Change:      members[0].Change,
		Repository:  options.Repository.Name,
		Dir:         options.Worktree,
		Delta:       members[0].Delta,
		FromSpec:    targets[0].fromSpec,
		ToSpec:      targets[0].toSpec,
		Observed:    targets[0].observed,
		ContextDoc:  targets[0].contextDoc,
		Verify:      options.Repository.Spec.Verify,
		Instruction: options.Instruction,
		Budget:      options.Budget,
	}
	for _, entry := range targets {
		request.Members = append(request.Members, agent.RealizeMember{
			Context:    entry.member.Context,
			Change:     entry.member.Change,
			Delta:      entry.member.Delta,
			FromSpec:   entry.fromSpec,
			ToSpec:     entry.toSpec,
			Observed:   entry.observed,
			ContextDoc: entry.contextDoc,
		})
	}
	agentResult, err := options.Agent.Realize(ctx, request)
	result.Agent = agentResult
	if err != nil {
		return result, err
	}

	if err := runGates(ctx, options.Repository, options.Worktree, options.VerifyTimeout, options.AcceptanceTimeout, &result); err != nil {
		return result, err
	}

	message := commitMessage(members, oabranch.BranchFor(options.Repository.Name, branch, oagit.Store{Repo: repoPath}.DefaultBranch(ctx)), options.Repository.Spec.Acceptance, result.Acceptance)
	commit, err := gitrepo.CommitAll(ctx, options.Worktree, message)
	if err != nil {
		return result, err
	}
	landedBase := options.Base
	if commit != "" {
		if tip, tipErr := gitrepo.CommitOf(ctx, repoPath, "refs/heads/"+branch); tipErr == nil && tip != options.Base {
			rebased, err := gitrepo.RebaseOnto(ctx, options.Worktree, tip)
			if err != nil {
				return result, fmt.Errorf("realize: %s moved under the change and the change does not rebase onto it: %w", branch, err)
			}
			if err := runGates(ctx, options.Repository, options.Worktree, options.VerifyTimeout, options.AcceptanceTimeout, &result); err != nil {
				return result, err
			}
			commit = rebased
			landedBase = tip
		}
		files, err := gitrepo.ChangedFiles(ctx, options.Worktree, landedBase, commit)
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

	if err := settle(ctx, options, namespace, adopts(targets)); err != nil {
		return result, err
	}
	return result, nil
}

func adopts(targets []target) map[string]*spec.SystemContextSpec {
	out := make(map[string]*spec.SystemContextSpec, len(targets))
	for index := range targets {
		snapshot := targets[index].toSpec
		out[targets[index].member.Context] = &snapshot
	}
	return out
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
	return settle(ctx, options, namespace, map[string]*spec.SystemContextSpec{options.Context: &adopted})
}

func settle(ctx context.Context, options Options, namespace string, adopted map[string]*spec.SystemContextSpec) error {
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
		AdoptAll:       adopted,
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

func runGates(ctx context.Context, repository *spec.Repository, dir string, verifyTimeout, acceptanceTimeout time.Duration, result *Result) error {
	exitCode, output := verify(ctx, repository.Spec.Verify, dir, verifyTimeout)
	result.VerifyExitCode = exitCode
	result.VerifyOutput = output
	if exitCode != 0 {
		return &VerifyError{Command: repository.Spec.Verify, ExitCode: exitCode, Output: output}
	}
	results := RunAcceptance(ctx, repository.Spec.Acceptance, dir, acceptanceTimeout)
	result.Acceptance = results
	if blocked, ok := spec.AcceptanceBlocked(repository.Spec.Acceptance, results); ok {
		return &AcceptanceError{Result: blocked, Gated: true}
	}
	return nil
}

func RunAcceptance(ctx context.Context, steps []spec.AcceptanceStep, dir string, timeout time.Duration) []spec.AcceptanceResult {
	results := make([]spec.AcceptanceResult, 0, len(steps))
	for _, step := range steps {
		results = append(results, runAcceptanceStep(ctx, step, dir, timeout))
	}
	return results
}

func runAcceptanceStep(ctx context.Context, step spec.AcceptanceStep, dir string, fallback time.Duration) spec.AcceptanceResult {
	result := spec.AcceptanceResult{Name: step.Name}
	if len(step.Command) == 0 {
		result.ExitCode = -1
		result.OutputTail = "the step names no command"
		return result
	}
	timeout := fallback
	if step.TimeoutSeconds > 0 {
		timeout = time.Duration(step.TimeoutSeconds) * time.Second
	}
	if timeout <= 0 {
		timeout = DefaultAcceptanceTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	log, err := os.CreateTemp("", "specd-acceptance-*.log")
	if err != nil {
		result.ExitCode = -1
		result.OutputTail = err.Error()
		return result
	}
	defer func() {
		name := log.Name()
		_ = log.Close()
		_ = os.Remove(name)
	}()
	process := exec.Command(step.Command[0], step.Command[1:]...)
	process.Dir = dir
	process.WaitDelay = WaitDelay
	process.Env = stepEnv(step.Env)
	process.Stdout = log
	process.Stderr = log
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	started := time.Now()
	err = process.Start()
	if err == nil {
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		select {
		case err = <-done:
		case <-runCtx.Done():
			killProcessGroup(process.Process.Pid)
			err = <-done
		}
		killProcessGroup(process.Process.Pid)
	}
	result.DurationSeconds = time.Since(started).Seconds()
	output, _ := os.ReadFile(log.Name())
	text := string(output)
	switch exitErr, ok := errors.AsType[*exec.ExitError](err); {
	case err == nil:
		result.Passed = true
	case ok:
		result.ExitCode = exitErr.ExitCode()
	default:
		result.ExitCode = -1
		text += "\n" + err.Error()
	}
	result.OutputTail = tail(text)
	return result
}

func killProcessGroup(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

func stepEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := extra[name]; replaced {
			continue
		}
		out = append(out, entry)
	}
	return append(out, envPairs(extra)...)
}

func envPairs(environment map[string]string) []string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+environment[key])
	}
	return pairs
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

func commitMessage(members []Member, archBranch string, steps []spec.AcceptanceStep, acceptance []spec.AcceptanceResult) string {
	message := fmt.Sprintf("realize %s: %s\n\n", members[0].Context, deltaSummary(batchDelta(members)))
	for _, member := range members {
		if member.Change != "" {
			message += oabranch.SpecChangeTrailer + ": " + member.Change + "\n"
		}
	}
	for _, line := range spec.AcceptanceTrailerLines(steps, acceptance) {
		message += oabranch.AcceptanceTrailer + ": " + line + "\n"
	}
	if archBranch != "" {
		message += oabranch.OpenArchitectureTrailer + ": " + archBranch + "\n"
	}
	return message
}

func batchDelta(members []Member) spec.Delta {
	combined := spec.Delta{}
	for _, member := range members {
		combined.Requirements = append(combined.Requirements, member.Delta.Requirements...)
		combined.Interfaces = append(combined.Interfaces, member.Delta.Interfaces...)
	}
	return combined
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
