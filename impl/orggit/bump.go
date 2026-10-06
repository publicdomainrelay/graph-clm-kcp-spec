package orggit

import (
	"context"
	"fmt"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
)

// BumpOptions controls Bump.
type BumpOptions struct {
	// Change is recorded as the Spec-Change trailer.
	Change string

	// Body is free text under the subject.
	Body string

	// AllowUnpublished lets a pointer move to a commit no remote has yet. The
	// default refuses: such a root commit cannot be used by anyone else.
	AllowUnpublished bool
}

// Bump records the current HEAD of the named members as their pins and commits
// the move on the root's current branch, with Member trailers. Only the
// gitlinks are committed: other staged changes of the root stay staged.
func (r *Root) Bump(ctx context.Context, names []string, options BumpOptions) (string, []org.Bump, error) {
	states, err := r.Members(ctx, MembersOptions{})
	if err != nil {
		return "", nil, err
	}
	var bumps []org.Bump
	var paths []string
	for _, state := range states {
		if !wanted(names, state) {
			continue
		}
		if !state.Initialized {
			return "", nil, fmt.Errorf("orggit: %s is not checked out", state.Path)
		}
		if state.Head == state.CodeCommit {
			continue
		}
		if !options.AllowUnpublished && !state.Shallow {
			published, err := r.published(ctx, state)
			if err != nil {
				return "", nil, err
			}
			if !published {
				return "", nil, fmt.Errorf("orggit: %s HEAD %s is on no remote branch; push it first (or allow unpublished)", state.Path, org.Short(state.Head))
			}
		}
		probe := state
		probe.CodeCommit = state.Head
		probe.Arch, probe.Policy = nil, nil
		if err := r.resolveSpec(ctx, &probe, r.MemberDir(state.Member)); err != nil {
			return "", nil, err
		}
		bump := org.Bump{Member: state.Name, Path: state.Path, From: state.CodeCommit, To: state.Head}
		if probe.Arch != nil && probe.State == org.StateResolved {
			bump.Spec = probe.Arch.Commit
		}
		bumps = append(bumps, bump)
		paths = append(paths, state.Path)
	}
	if len(bumps) == 0 {
		return "", nil, nil
	}
	if _, err := r.run(ctx, r.Dir, append([]string{"add", "--"}, paths...)...); err != nil {
		return "", nil, err
	}
	message := org.Message(bumps, options.Change, options.Body)
	if _, err := r.run(ctx, r.Dir, append([]string{"commit", "-q", "-m", message, "--only", "--"}, paths...)...); err != nil {
		return "", nil, err
	}
	commit, err := r.run(ctx, r.Dir, "rev-parse", "HEAD")
	return commit, bumps, err
}

func wanted(names []string, state org.MemberState) bool {
	if len(names) == 0 {
		return true
	}
	for _, name := range names {
		if name == state.Name || name == state.Path || strings.TrimSuffix(name, "/") == state.Path {
			return true
		}
	}
	return false
}

func (r *Root) published(ctx context.Context, state org.MemberState) (bool, error) {
	out, err := r.run(ctx, r.MemberDir(state.Member), "branch", "-r", "--contains", state.Head)
	if err != nil {
		return false, err
	}
	return out != "", nil
}
