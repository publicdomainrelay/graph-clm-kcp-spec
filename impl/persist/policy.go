package persist

import (
	"context"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
)

// guardedRequirements reads the repository's policy branch and names the
// requirement ids a constraint template enforces, by context, so CHANGES.md
// marks a requirement a policy guards. A repository without a policy branch
// guards nothing.
func guardedRequirements(ctx context.Context, store oagit.Store, repository *spec.Repository) map[string][]string {
	if repository.Status.ResolvedPath == "" && repository.WorkPath() == "" {
		return nil
	}
	branch := repository.Name
	if repository.Spec.Policy != nil && repository.Spec.Policy.Branch != "" {
		branch = repository.Spec.Policy.Branch
	}
	ref := policy.RefFor(branch, repository.Spec.Branch, store.DefaultBranch(ctx))
	library, _, err := policygit.Read(ctx, store, ref)
	if err != nil {
		return nil
	}
	return policy.GuardedByContext(library)
}
