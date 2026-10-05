package specd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/populate"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

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

	if source := repository.Source(); source.Path == "" && source.Git == nil {
		return 0, nil
	}

	if result := spec.ValidateRepository(repository); !result.OK() {
		return c.opts.Resync, c.setRepositoryCondition(ctx, repository, namespace,
			metav1.ConditionFalse, specapi.ReasonSourceInvalid, result.Err().Error())
	}

	path, commit, err := c.resolveSource(ctx, repository, namespace)
	if err != nil {
		c.log.Error("could not resolve the repository source", "repository", name, "err", err)
		if conditionErr := c.failRepository(ctx, repository, namespace, specapi.ReasonCloneFailed, err.Error()); conditionErr != nil {
			return 0, conditionErr
		}
		return c.opts.Resync, nil
	}

	branch := c.branchStatus(ctx, repository)
	if err := c.setBranchCondition(ctx, repository, namespace, branch); err != nil {
		return 0, err
	}
	if branch.Mismatch {
		c.log.Info("the checkout is on another branch; the repository is left alone",
			"repository", name, "path", path, "branch", repository.Spec.Branch, "message", branch.Message)
		return c.opts.Resync, nil
	}

	c.reconcileRepositoryPolicy(ctx, namespace, repository, path, commit)

	running, err := c.anyRunningRealize(ctx, namespace)
	if err != nil {
		return 0, err
	}
	if running {
		return c.opts.Resync, nil
	}

	request := repository.Annotations[specapi.PopulateRequestAnnotation]
	result, err := populate.Run(ctx, populate.Options{
		Cluster:       c.client,
		Namespace:     namespace,
		Repository:    repository,
		Path:          path,
		Index:         c.needsIndex(repository, path, commit, request),
		Commit:        commit,
		Tool:          c.opts.Tool,
		Writer:        c.opts.Graph,
		MaxConcurrent: c.opts.MaxConcurrentSummaries,
		MaxAttempts:   c.opts.MaxAttempts,
	})
	if err != nil {
		c.log.Error("populate failed", "repository", name, "path", path, "err", err)
		if conditionErr := c.failRepository(ctx, repository, namespace, specapi.ReasonIndexFailed, err.Error()); conditionErr != nil {
			c.log.Error("could not record the failed populate", "repository", name, "err", conditionErr)
		}
		return 0, err
	}
	for _, contextResult := range result.Contexts {
		if contextResult.Skipped {
			c.log.Warn("a context of another repository shares this name",
				"repository", name, "systemcontext", contextResult.Name, "reason", contextResult.Reason)
		}
	}
	level := slog.LevelDebug
	if repository.Status.Phase != result.Phase || repository.Status.IndexedCommit != commit || result.Raised > 0 {
		level = slog.LevelInfo
	}
	c.log.Log(ctx, level, "repository populated",
		"repository", name, "path", path, "commit", commit, "phase", result.Phase,
		"contexts", result.Total, "summarized", result.Summarized, "raised", result.Raised)
	return c.opts.Resync, nil
}

func (c *Controller) resolveSource(ctx context.Context, repository *spec.Repository, namespace string) (string, string, error) {
	source := repository.Source()
	switch {
	case source.Git != nil:
		dir, err := c.cacheDirFor(repository.Name)
		if err != nil {
			return "", "", err
		}
		if repository.Status.ResolvedPath != dir || repository.Status.Phase == "" {
			if err := c.setRepositoryPhase(ctx, repository, namespace, specapi.PhaseCloning); err != nil {
				return "", "", err
			}
		}
		commit, err := gitrepo.EnsureCheckout(ctx, source.Git.URL, source.Git.Ref, dir)
		if err != nil {
			return "", "", err
		}
		return dir, commit, nil
	case source.Path != "":
		path, err := filepath.Abs(source.Path)
		if err != nil {
			return "", "", fmt.Errorf("specd: resolve %s: %w", source.Path, err)
		}
		if _, err := os.Stat(path); err != nil {
			return "", "", fmt.Errorf("specd: %s: %w", path, err)
		}
		commit, _ := gitrepo.Head(ctx, path)
		return path, commit, nil
	}
	return "", "", fmt.Errorf("specd: %s names no source", repository.Name)
}

func (c *Controller) needsIndex(repository *spec.Repository, path, commit, request string) bool {
	if repository.Status.ResolvedPath != path {
		return true
	}
	if request != "" && request != repository.Status.PopulateRequest {
		return true
	}
	switch repository.Status.Phase {
	case "":
		return true
	case specapi.PhasePopulated, specapi.PhasePopulating, specapi.PhaseFailed:
	default:
		return true
	}
	if repository.Status.IndexedCommit == "" && commit != "" {
		return true
	}
	return commit != "" && commit != repository.Status.IndexedCommit
}

func (c *Controller) cacheDirFor(name string) (string, error) {
	root := c.opts.CacheDir
	if root == "" {
		root = DefaultCacheDir
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("specd: resolve the cache dir %s: %w", root, err)
	}
	return filepath.Join(absolute, name), nil
}

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

func (c *Controller) failRepository(ctx context.Context, repository *spec.Repository, namespace, reason, message string) error {
	conditions := condition.Copy(repository.Status.Conditions)
	condition.SetFalse(&conditions, repository.GetGeneration(), specapi.ConditionIndexed, reason, message)
	populated := condition.Copy(conditions)
	condition.SetFalse(&populated, repository.GetGeneration(), specapi.ConditionPopulated, reason, message)
	status := map[string]any{
		"phase":      specapi.PhaseFailed,
		"conditions": populated,
	}
	if specapi.StatusMatches(repository.Status, status) {
		return nil
	}
	_, err := c.client.PatchStatus(ctx, specapi.RepositoryGVR, namespace, repository.Name, status)
	return err
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

func (c *Controller) setRepositoryPhase(ctx context.Context, repository *spec.Repository, namespace, phase string) error {
	if repository.Status.Phase == phase {
		return nil
	}
	_, err := c.client.PatchStatus(ctx, specapi.RepositoryGVR, namespace, repository.Name,
		map[string]any{"phase": phase})
	return err
}
