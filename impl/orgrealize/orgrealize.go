// Package orgrealize carries out a change that spans repositories of an org
// root. Each step realizes one member's context in that member's own
// checkout, commits there, and only when every step has succeeded does the root
// record the new pointers in one commit. A failing step puts every member back
// where it started and leaves the root untouched.
package orgrealize

import (
	"context"
	"fmt"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/orggit"
)

// Step is one member's share of a change.
type Step struct {
	// Member is the member's name or path.
	Member string `json:"member"`

	// Context is the SystemContext of that member the agent realizes.
	Context string `json:"context"`

	// Instruction is free text for the agent.
	Instruction string `json:"instruction,omitempty"`
}

// Plan is a change across repositories.
type Plan struct {
	// Change names the change; it is the Spec-Change trailer of every commit.
	Change string `json:"change"`

	Steps []Step `json:"steps"`

	// Body is the root commit's body.
	Body string `json:"body,omitempty"`
}

// Options wires the executor.
type Options struct {
	Root *orggit.Root

	Agent agent.Agent

	// Verify gates each member's commit, like the single repository path's
	// verify step. A failure rolls the whole change back.
	Verify func(ctx context.Context, member org.MemberState, dir string) error

	// Push publishes each member's change branch to its origin before the root
	// records the pointer, so no pin is dangling. Without it the pointers are
	// recorded for commits only this machine has.
	Push bool

	// RootBranch makes the root commit on a new branch of that name instead of
	// the root's current branch.
	RootBranch string
}

// StepResult is what one step did.
type StepResult struct {
	Member string `json:"member"`

	Context string `json:"context"`

	Branch string `json:"branch"`

	From string `json:"from"`

	To string `json:"to"`

	Files []string `json:"files,omitempty"`

	Summary string `json:"summary,omitempty"`
}

// Result is what the change did.
type Result struct {
	Steps []StepResult `json:"steps"`

	RootCommit string `json:"rootCommit,omitempty"`

	Bumps []org.Bump `json:"bumps,omitempty"`

	// Unpublished lists members whose new commit no remote has.
	Unpublished []string `json:"unpublished,omitempty"`
}

// BranchFor is the change branch used in every member.
func BranchFor(change string) string { return "specd/" + sanitize(change) }

func sanitize(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

// Run executes the plan. On error every member is restored and the root has no
// new commit.
func Run(ctx context.Context, options Options, plan Plan) (Result, error) {
	if options.Root == nil || options.Agent == nil {
		return Result{}, fmt.Errorf("orgrealize: a root and an agent are required")
	}
	if plan.Change == "" || len(plan.Steps) == 0 {
		return Result{}, fmt.Errorf("orgrealize: a plan needs a change name and at least one step")
	}
	states, err := options.Root.Members(ctx, orggit.MembersOptions{})
	if err != nil {
		return Result{}, err
	}
	// Resolve and check every member before the first edit, so a mistake in
	// step three does not leave step one half done.
	targets := make([]org.MemberState, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		state, ok := find(states, step.Member)
		switch {
		case !ok:
			return Result{}, fmt.Errorf("orgrealize: %q is not a member of %s", step.Member, options.Root.Name)
		case !state.Initialized:
			return Result{}, fmt.Errorf("orgrealize: %s is not checked out", state.Path)
		case state.Dirty:
			return Result{}, fmt.Errorf("orgrealize: %s has uncommitted changes; commit or stash them first", state.Path)
		}
		for _, other := range targets {
			if other.Path == state.Path {
				return Result{}, fmt.Errorf("orgrealize: %s appears in two steps; merge them", state.Path)
			}
		}
		targets = append(targets, state)
	}

	branch := BranchFor(plan.Change)
	var done []doneStep
	rootBranch := options.Root.CurrentBranch(ctx)
	switched := false
	rollback := func() {
		for index := len(done) - 1; index >= 0; index-- {
			done[index].restore(ctx, options.Root, branch)
		}
		if switched && rootBranch != "" {
			options.Root.Git(ctx, options.Root.Dir, "checkout", "-q", rootBranch)
			options.Root.Git(ctx, options.Root.Dir, "branch", "-q", "-D", options.RootBranch)
		}
	}

	result := Result{}
	for index, step := range plan.Steps {
		state := targets[index]
		dir := options.Root.MemberDir(state.Member)
		start := state.Head
		started := doneStep{state: state, dir: dir, start: start, startBranch: state.Branch}
		if _, err := options.Root.Git(ctx, dir, "checkout", "-q", "-B", branch, start); err != nil {
			rollback()
			return Result{}, err
		}
		done = append(done, started)

		realized, err := options.Agent.Realize(ctx, agent.RealizeRequest{
			Context:     step.Context,
			Change:      plan.Change,
			Repository:  state.Name,
			Dir:         dir,
			Instruction: step.Instruction,
			Attempt:     1,
		})
		if err != nil {
			rollback()
			return Result{}, fmt.Errorf("orgrealize: agent on %s: %w", state.Path, err)
		}
		if options.Verify != nil {
			if err := options.Verify(ctx, state, dir); err != nil {
				rollback()
				return Result{}, fmt.Errorf("orgrealize: verify %s: %w", state.Path, err)
			}
		}
		message := fmt.Sprintf("%s: %s\n\n%s: %s\nOrg-Root: %s\n", state.Name, firstNonEmpty(realized.Summary, "realize "+step.Context),
			org.SpecChangeTrailer, plan.Change, options.Root.Name)
		commit, err := gitrepo.CommitAll(ctx, dir, message)
		if err != nil {
			rollback()
			return Result{}, err
		}
		if commit == "" {
			rollback()
			return Result{}, fmt.Errorf("orgrealize: the agent changed nothing in %s", state.Path)
		}
		if options.Push {
			if _, err := options.Root.Git(ctx, dir, "push", "-q", "origin", branch); err != nil {
				rollback()
				return Result{}, err
			}
		} else {
			result.Unpublished = append(result.Unpublished, state.Name)
		}
		result.Steps = append(result.Steps, StepResult{
			Member: state.Name, Context: step.Context, Branch: branch,
			From: start, To: commit, Files: realized.Files, Summary: realized.Summary,
		})
	}

	if options.RootBranch != "" {
		if _, err := options.Root.Git(ctx, options.Root.Dir, "checkout", "-q", "-B", options.RootBranch); err != nil {
			rollback()
			return Result{}, err
		}
		switched = true
	}
	names := make([]string, 0, len(targets))
	for _, state := range targets {
		names = append(names, state.Path)
	}
	commit, bumps, err := options.Root.Bump(ctx, names, orggit.BumpOptions{
		Change: plan.Change, Body: plan.Body, AllowUnpublished: !options.Push,
	})
	if err != nil {
		rollback()
		return Result{}, err
	}
	result.RootCommit, result.Bumps = commit, bumps
	return result, nil
}

// doneStep remembers where a member was, so a failure can put it back.
type doneStep struct {
	state       org.MemberState
	dir         string
	start       string
	startBranch string
}

func (d doneStep) restore(ctx context.Context, root *orggit.Root, branch string) {
	target := d.start
	if d.startBranch != "" {
		target = d.startBranch
	}
	// The change branch is ours: the member was clean when the step began, so
	// whatever is in the tree now is the step's own work.
	root.Git(ctx, d.dir, "reset", "-q", "--hard")
	root.Git(ctx, d.dir, "clean", "-fdq")
	root.Git(ctx, d.dir, "checkout", "-q", target)
	if d.startBranch != "" {
		root.Git(ctx, d.dir, "reset", "-q", "--hard", d.start)
	}
	root.Git(ctx, d.dir, "branch", "-q", "-D", branch)
}

func find(states []org.MemberState, name string) (org.MemberState, bool) {
	for _, state := range states {
		if state.Name == name || state.Path == name {
			return state, true
		}
	}
	return org.MemberState{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
