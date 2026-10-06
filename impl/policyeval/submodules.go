package policyeval

import (
	"context"
	"fmt"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/orggit"
)

// EffectiveMembers is the member list a library is evaluated with: its own
// `members:` plus, when `submodules.members` is set, one member per gitlink of
// codeDir. A derived member's ref is the gitlink's pinned commit, so the pin is
// the superproject's and no second pin is kept. Where a submodule is checked
// out, its directory is the member's source; paths given by the caller win.
func EffectiveMembers(ctx context.Context, library policy.Library, codeDir string, paths map[string]string) ([]policy.Member, map[string]string, error) {
	config := library.Manifest.Submodules
	if config == nil || !config.Members {
		return library.Manifest.Members, paths, nil
	}
	root := &orggit.Root{Dir: codeDir, Name: library.Manifest.Repository}
	states, err := root.Members(ctx, orggit.MembersOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("policyeval: read the submodules of %s: %w", codeDir, err)
	}
	members := make([]org.Member, 0, len(states))
	merged := map[string]string{}
	for _, state := range states {
		members = append(members, state.Member)
		if state.Initialized {
			merged[state.Name] = root.MemberDir(state.Member)
		}
	}
	for name, path := range paths {
		merged[name] = path
	}
	return org.PolicyMembers(members, *config, library.Manifest.Members), merged, nil
}

// MemberEvaluation is what a rollup hands the evaluator: one initialized
// submodule that has a policy branch.
type MemberEvaluation struct {
	State org.MemberState

	// Dir is the member's checkout.
	Dir string

	// PolicyRef is the ref of the member's policy branch that resolved.
	PolicyRef string
}

// MemberEvaluator evaluates a member's own library against that member's own
// tree. The caller owns the engine, so this package stays free of it.
type MemberEvaluator func(ctx context.Context, member MemberEvaluation) (policy.Report, error)

// MemberRollup evaluates every submodule that carries its own policy branch,
// when the library asks for it (`submodules.policies`), and returns one row
// each. A failed evaluation is a row with a message, not an error: one broken
// member must not hide the rest of the org.
func MemberRollup(ctx context.Context, library policy.Library, codeDir string, evaluate MemberEvaluator) ([]policy.MemberRollup, error) {
	config := library.Manifest.Submodules
	if config == nil || !config.Policies || evaluate == nil {
		return nil, nil
	}
	root := &orggit.Root{Dir: codeDir, Name: library.Manifest.Repository}
	states, err := root.Members(ctx, orggit.MembersOptions{})
	if err != nil {
		return nil, fmt.Errorf("policyeval: read the submodules of %s: %w", codeDir, err)
	}
	var rows []policy.MemberRollup
	for _, state := range states {
		if !state.Initialized || state.Policy == nil || !selectedPath(state.Path, *config) {
			continue
		}
		row := policy.MemberRollup{Name: state.Name, Path: state.Path, Commit: state.Head, PolicyCommit: state.Policy.Commit}
		if state.Head != state.CodeCommit {
			row.Pinned = state.CodeCommit
		}
		ref, _ := root.PolicyRef(ctx, state.Member)
		report, err := evaluate(ctx, MemberEvaluation{State: state, Dir: root.MemberDir(state.Member), PolicyRef: ref})
		if err != nil {
			row.Message = err.Error()
		} else {
			row.Totals = report.Totals
			row.Violations = report.Violations
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func selectedPath(path string, config policy.Submodules) bool {
	return len(org.PolicyMembers([]org.Member{{Name: path, Path: path}}, config, nil)) == 1
}
