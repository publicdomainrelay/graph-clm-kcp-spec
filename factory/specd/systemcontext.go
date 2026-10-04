package specd

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func (c *Controller) reconcileSystemContext(ctx context.Context, namespace, name string) (time.Duration, error) {
	object, err := c.client.Get(ctx, specapi.SystemContextGVR, namespace, name)
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
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return 0, fmt.Errorf("specd: %s is not a SystemContext", name)
	}

	decision, conditions := specsync.Conditions(specsync.Input{
		Name:              name,
		Generation:        systemContext.GetGeneration(),
		Spec:              systemContext.Spec,
		Observed:          systemContext.Status.Observed,
		SyncedFingerprint: systemContext.Status.SyncedFingerprint,
	}, systemContext.Status.Conditions)

	status := map[string]any{
		"observedGeneration": systemContext.GetGeneration(),
		"conditions":         conditions,
	}
	if !specapi.StatusMatches(systemContext.Status, status) {
		if _, err := c.client.PatchStatus(ctx, specapi.SystemContextGVR, namespace, name, status); err != nil {
			return 0, err
		}
		c.log.Info("conditions updated",
			"systemcontext", name, "generation", systemContext.GetGeneration(), "drifted", decision.Drifted)
	}
	return c.reconcileChanges(ctx, namespace, systemContext, decision)
}

func (c *Controller) reconcileChanges(
	ctx context.Context,
	namespace string,
	systemContext *spec.SystemContext,
	decision specsync.Decision,
) (time.Duration, error) {
	fromCommit := systemContext.Status.SyncedCommit
	toCommit := systemContext.Status.ObservedCommit
	driftDue := specsync.CodeToSpecDue(decision.Drifted, fromCommit, toCommit)

	specHash, err := spec.HashSystemContextSpec(systemContext.Spec)
	if err != nil {
		return 0, err
	}
	editDue := specsync.SpecEditDue(specHash, systemContext.Status.RealizedSpecHash,
		systemContext.Annotations[specapi.OriginHashAnnotation])
	if !driftDue && !editDue {
		return 0, nil
	}

	changes, err := c.changesFor(ctx, namespace, systemContext.Name)
	if err != nil {
		return 0, err
	}
	unfinished := map[string]bool{}
	taken := make([]string, 0, len(changes))
	for _, change := range changes {
		taken = append(taken, change.Name)
		switch change.Status.Phase {
		case specapi.PhasePending, specapi.PhaseRunning:
			unfinished[change.Spec.Direction] = true
		}
	}

	requeue := time.Duration(0)
	if driftDue && !unfinished[specapi.DirectionCodeToSpec] {
		base := spec.ChangeNameCodeToSpec(systemContext.Name, fromCommit, toCommit)
		wait, ready := c.retryDelay(changes, taken, base)
		if ready && episodeSucceeded(changes, base) {
			ready = false
		}
		switch {
		case !ready:
			c.log.Warn("not raising another code to spec change: the attempt cap is reached",
				"systemcontext", systemContext.Name, "episode", base, "maxAttempts", c.opts.MaxAttempts)
		case wait > 0:
			requeue = max(requeue, wait)
		default:
			observedDelta := delta.DiffObserved(systemContext.Status.SyncedObserved, systemContext.Status.Observed)
			change := &spec.SpecChange{
				ObjectMeta: metav1.ObjectMeta{
					Name:      spec.NextChangeName(taken, base),
					Namespace: namespace,
				},
				Spec: spec.SpecChangeSpec{
					SystemContext: systemContext.Name,
					Direction:     specapi.DirectionCodeToSpec,
					FromCommit:    fromCommit,
					ToCommit:      toCommit,
					Delta:         &observedDelta,
				},
			}
			if err := c.createChange(ctx, change); err != nil {
				return 0, err
			}
		}
	}

	if editDue && !unfinished[specapi.DirectionSpecToCode] {
		base := spec.ChangeNameSpecToCode(systemContext.Name, specHash)
		wait, ready := c.retryDelay(changes, taken, base)
		if ready && episodeSucceeded(changes, base) {
			ready = false
		}
		switch {
		case !ready:
			c.log.Warn("not raising another spec to code change: the attempt cap is reached",
				"systemcontext", systemContext.Name, "episode", base, "maxAttempts", c.opts.MaxAttempts)
		case wait > 0:
			requeue = max(requeue, wait)
		default:
			specDelta := delta.Diff(realizedSpecOf(systemContext), systemContext.Spec)
			change := &spec.SpecChange{
				ObjectMeta: metav1.ObjectMeta{
					Name:      spec.NextChangeName(taken, base),
					Namespace: namespace,
				},
				Spec: spec.SpecChangeSpec{
					SystemContext: systemContext.Name,
					Direction:     specapi.DirectionSpecToCode,
					FromSpecHash:  systemContext.Status.RealizedSpecHash,
					ToSpecHash:    specHash,
					Delta:         &specDelta,
				},
			}
			if err := c.createChange(ctx, change); err != nil {
				return 0, err
			}
		}
	}
	return requeue, nil
}

func (c *Controller) retryDelay(changes []spec.SpecChange, taken []string, base string) (time.Duration, bool) {
	attempts := spec.AttemptCount(taken, base)
	if attempts == 0 {
		return 0, true
	}
	return specsync.RetryBackoff(attempts, newestFailure(changes, base), time.Now(), c.opts.RetryBackoff, c.opts.MaxAttempts)
}

func episodeSucceeded(changes []spec.SpecChange, base string) bool {
	for _, change := range changes {
		if change.Status.Phase != specapi.PhaseSucceeded {
			continue
		}
		if change.Name == base || strings.HasPrefix(change.Name, base+"-a") {
			return true
		}
	}
	return false
}

func newestFailure(changes []spec.SpecChange, base string) time.Time {
	newest := time.Time{}
	for _, change := range changes {
		if change.Status.Phase != specapi.PhaseFailed {
			continue
		}
		if change.Name != base && !strings.HasPrefix(change.Name, base+"-a") {
			continue
		}
		if created := change.GetCreationTimestamp().Time; created.After(newest) {
			newest = created
		}
	}
	return newest
}

func episodeBase(change *spec.SpecChange) string {
	switch change.Spec.Direction {
	case specapi.DirectionCodeToSpec:
		return spec.ChangeNameCodeToSpec(change.Spec.SystemContext, change.Spec.FromCommit, change.Spec.ToCommit)
	case specapi.DirectionSpecToCode:
		return spec.ChangeNameSpecToCode(change.Spec.SystemContext, change.Spec.ToSpecHash)
	}
	return change.Name
}

func (c *Controller) attemptsTaken(ctx context.Context, namespace string, change *spec.SpecChange) int {
	changes, err := c.changesFor(ctx, namespace, change.Spec.SystemContext)
	if err != nil {
		return 1
	}
	taken := make([]string, 0, len(changes))
	for _, recorded := range changes {
		taken = append(taken, recorded.Name)
	}
	return spec.AttemptCount(taken, episodeBase(change))
}

func (c *Controller) changesFor(ctx context.Context, namespace, systemContext string) ([]spec.SpecChange, error) {
	listed, err := c.client.List(ctx, specapi.SpecChangeGVR, namespace)
	if err != nil {
		return nil, err
	}
	out := []spec.SpecChange{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok {
			continue
		}
		if change.Spec.SystemContext == systemContext {
			out = append(out, *change)
		}
	}
	return out, nil
}
