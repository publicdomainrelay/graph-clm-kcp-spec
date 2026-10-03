package specd

import (
	"context"
	"fmt"
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

// reconcileRepository is the one manifest that populates an unknown codebase:
// resolve the source (clone a git url into the cache), index it, create one
// SystemContext per partition, and, when the Repository asks for it, raise one
// CodeToSpec change per context whose spec is still empty. The resync keeps
// asking, because neither a commit nor a remote HEAD is visible to the API
// server.
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
	c.log.Info("repository populated",
		"repository", name, "path", path, "commit", commit, "phase", result.Phase,
		"contexts", result.Total, "summarized", result.Summarized, "raised", result.Raised)
	return c.opts.Resync, nil
}

// resolveSource turns a Repository into a working tree. A path is used as it
// is, if it is there; a git url is cloned into the controller's cache and
// fetched up to date there. The returned commit is empty for a tree that is not
// a git working tree, which is allowed: such a tree is indexed once and cannot
// drift.
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

// needsIndex decides whether the working tree has to be indexed again. A
// populate request, an unknown or moved resolved path, a first run, a missing
// indexed commit and a moved HEAD all ask for it; a pipeline that is merely
// Populating or that already ran over this HEAD does not.
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

// cacheDirFor is where one Repository's git source is cloned. The name is a
// DNS-1123 label by the time it is here, so it cannot escape the cache.
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

// failRepository records a phase and a condition for a source that could not be
// resolved or indexed. The Indexed condition carries the reason, so a reader
// sees what went wrong without the message having to be parsed.
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
