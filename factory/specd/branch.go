package specd

import (
	"context"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

func (c *Controller) branchStatus(ctx context.Context, repository *spec.Repository) specsync.BranchStatus {
	path := repository.WorkPath()
	if path == "" || repository.Spec.Branch == "" {
		return specsync.BranchStatus{}
	}
	head, err := gitrepo.Branch(ctx, path)
	if err != nil {
		return specsync.BranchStatus{}
	}
	return specsync.BranchCheck(head, repository.Spec.Branch)
}

func (c *Controller) setBranchCondition(ctx context.Context, repository *spec.Repository, namespace string, status specsync.BranchStatus) error {
	conditions := condition.Copy(repository.Status.Conditions)
	if status.Mismatch {
		condition.SetTrue(&conditions, repository.GetGeneration(), specapi.ConditionBranchMismatch, specapi.ReasonBranchMismatch, status.Message)
	} else {
		condition.SetFalse(&conditions, repository.GetGeneration(), specapi.ConditionBranchMismatch, specapi.ReasonBranchMatches, "the checkout is on the repository's branch")
	}
	if specapi.StatusMatches(repository.Status, map[string]any{"conditions": conditions}) {
		return nil
	}
	_, err := c.client.PatchStatus(ctx, specapi.RepositoryGVR, namespace, repository.Name, map[string]any{"conditions": conditions})
	return err
}
