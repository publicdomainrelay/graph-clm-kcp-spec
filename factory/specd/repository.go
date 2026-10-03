package specd

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

// reconcileRepository keeps the index of one working tree at its HEAD. A new
// commit runs the phase 2 ingest, which rewrites the observed facts of every
// context and records the drift it finds. The resync keeps asking, because a
// commit is invisible to the API server.
func (c *Controller) reconcileRepository(ctx context.Context, namespace, name string) (time.Duration, error) {
	object, err := c.client.Get(ctx, specapi.RepositoryGVR, namespace, name)
	if err != nil {
		if kcpclient.IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return 0, err
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		return 0, fmt.Errorf("specd: %s is not a Repository", name)
	}

	if repository.Spec.Path == "" {
		return c.opts.Resync, c.setRepositoryCondition(ctx, repository, namespace,
			metav1.ConditionFalse, specapi.ReasonPathMissing, "spec.path is empty")
	}
	path, err := filepath.Abs(repository.Spec.Path)
	if err != nil {
		return 0, fmt.Errorf("specd: resolve %s: %w", repository.Spec.Path, err)
	}

	head, err := gitrepo.Head(ctx, path)
	if err != nil {
		message := err.Error()
		if conditionErr := c.setRepositoryCondition(ctx, repository, namespace,
			metav1.ConditionFalse, specapi.ReasonHeadUnavailable, message); conditionErr != nil {
			return 0, conditionErr
		}
		return c.opts.Resync, nil
	}
	if head == repository.Status.IndexedCommit {
		// Nothing to index, but a condition left False by an earlier failure
		// (a path that did not exist yet) has to come back to True.
		return c.opts.Resync, c.setRepositoryCondition(ctx, repository, namespace,
			metav1.ConditionTrue, specapi.ReasonIndexed, "the codegraph index is current")
	}

	// A realize moves the branch as part of its own work, and the ingest that
	// adopts it is the tail of that change. Indexing in between would report
	// the half-finished move as drift and raise the opposite change for it, so
	// the resync waits while a spec -> code change is running.
	running, err := c.anyRunningRealize(ctx, namespace)
	if err != nil {
		return 0, err
	}
	if running {
		return c.opts.Resync, nil
	}

	result, err := ingest.Run(ctx, c.client, ingest.Options{
		RepoPath:       path,
		RepositoryName: name,
		Namespace:      namespace,
		SpecPath:       path,
		Tool:           c.opts.Tool,
		Commit:         head,
		Writer:         c.opts.Graph,
	})
	if err != nil {
		c.log.Error("ingest failed", "repository", name, "path", path, "commit", head, "err", err)
		if conditionErr := c.setRepositoryCondition(ctx, repository, namespace,
			metav1.ConditionFalse, specapi.ReasonIndexFailed, err.Error()); conditionErr != nil {
			c.log.Error("could not record the failed ingest", "repository", name, "err", conditionErr)
		}
		return 0, err
	}
	for _, contextResult := range result.Contexts {
		if contextResult.Skipped {
			c.log.Warn("a context of another repository shares this name",
				"repository", name, "systemcontext", contextResult.Name, "reason", contextResult.Reason)
		}
	}
	c.log.Info("repository ingested",
		"repository", name, "path", path, "commit", head, "contexts", len(result.Contexts))
	return c.opts.Resync, nil
}

// anyRunningRealize reports whether a spec -> code change is mid-flight
// anywhere in the namespace. Only that direction moves the branch, so only it
// needs the index to stand still; a code -> spec change that is running says
// nothing about the tree. It is deliberately coarse about which repository: a
// realize is short, and waiting for it costs one list, while indexing beside it
// costs a drift report the controller would act on.
func (c *Controller) anyRunningRealize(ctx context.Context, namespace string) (bool, error) {
	listed, err := c.client.List(ctx, specapi.SpecChangeGVR, namespace)
	if err != nil {
		return false, err
	}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return false, err
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok {
			continue
		}
		if change.Status.Phase == specapi.PhaseRunning && change.Spec.Direction == specapi.DirectionSpecToCode {
			return true, nil
		}
	}
	return false, nil
}

func (c *Controller) setRepositoryCondition(
	ctx context.Context,
	repository *spec.Repository,
	namespace string,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	conditions := condition.Copy(repository.Status.Conditions)
	condition.Set(&conditions, repository.GetGeneration(), status, specapi.ConditionIndexed, reason, message)
	if specapi.StatusMatches(repository.Status, map[string]any{"conditions": conditions}) {
		return nil
	}
	_, err := c.client.PatchStatus(ctx, specapi.RepositoryGVR, namespace, repository.Name,
		map[string]any{"conditions": conditions})
	return err
}
