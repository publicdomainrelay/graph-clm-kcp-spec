package orggit

import (
	"context"
	"fmt"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

// FetchResult says what Fetch brought in for one member.
type FetchResult struct {
	Member string `json:"member"`

	Branches []string `json:"branches,omitempty"`

	PinFetched bool `json:"pinFetched,omitempty"`

	Message string `json:"message,omitempty"`
}

// Fetch brings each initialized member's architecture and policy branches, and
// the pinned commit when it is missing, into the member's own repository. A
// full recursive clone has them already; a shallow one has only the default
// branch. Branches are fetched by name, never all of them.
func (r *Root) Fetch(ctx context.Context) ([]FetchResult, error) {
	states, err := r.Members(ctx, MembersOptions{})
	if err != nil {
		return nil, err
	}
	var results []FetchResult
	for _, state := range states {
		if !state.Initialized {
			continue
		}
		result := FetchResult{Member: state.Name}
		dir := r.MemberDir(state.Member)
		listed, err := r.run(ctx, dir, "ls-remote", "--heads", "origin", oabranch.Prefix+"*", policy.BranchPrefix+"*")
		if err != nil {
			result.Message = "ls-remote failed: " + firstLine(err.Error())
			results = append(results, result)
			continue
		}
		for _, branch := range matching(listed, state.Candidates) {
			spec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
			if _, err := r.run(ctx, dir, "fetch", "-q", "--no-tags", "origin", spec); err != nil {
				result.Message = "fetch " + branch + ": " + firstLine(err.Error())
				continue
			}
			result.Branches = append(result.Branches, branch)
		}
		if !r.hasCommit(ctx, dir, state.CodeCommit) {
			if _, err := r.run(ctx, dir, "fetch", "-q", "--no-tags", "origin", state.CodeCommit); err == nil {
				result.PinFetched = true
			} else {
				result.Message = "pin " + org.Short(state.CodeCommit) + " cannot be fetched by id"
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// matching keeps the architecture and policy branches that belong to a member:
// open-architecture/<name> and open-architecture/<name>--<branch>, for any of
// the candidate names.
func matching(lsRemote string, candidates []string) []string {
	var out []string
	for _, line := range strings.Split(lsRemote, "\n") {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		branch := strings.TrimPrefix(ref, "refs/heads/")
		for _, name := range candidates {
			for _, prefix := range []string{oabranch.Prefix, policy.BranchPrefix} {
				if branch == prefix+name || strings.HasPrefix(branch, prefix+name+oabranch.BranchSeparator) {
					out = append(out, branch)
				}
			}
		}
	}
	return out
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

// CloneOptions controls Clone.
type CloneOptions struct {
	// Depth makes shallow clones of the root and of every member; zero is full.
	Depth int

	// Env is extra environment for git (identity, protocol.file.allow).
	Env []string
}

// Clone clones an org root with its submodules recursively and then fetches
// each member's architecture and policy branches. A full clone is the way to
// see how the root's pointers moved over its history.
func Clone(ctx context.Context, url, dir string, options CloneOptions) (*Root, error) {
	root := &Root{Env: options.Env}
	args := []string{"clone", "-q", "--recurse-submodules"}
	if options.Depth > 0 {
		args = append(args, "--depth", fmt.Sprint(options.Depth), "--shallow-submodules")
	}
	args = append(args, url, dir)
	if _, err := root.run(ctx, ".", args...); err != nil {
		return nil, err
	}
	opened, err := Open(ctx, dir)
	if err != nil {
		return nil, err
	}
	opened.Env = options.Env
	if _, err := opened.Fetch(ctx); err != nil {
		return nil, err
	}
	return opened, nil
}
