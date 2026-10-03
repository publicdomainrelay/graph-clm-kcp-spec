package specd

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// reconcileSpecChange keeps a change Pending and enforces the admission rule:
// at most one change may run per SystemContext. Agents take a change from
// Pending to Running in phases 5 and 6; until then nothing moves.
func (c *Controller) reconcileSpecChange(ctx context.Context, namespace, name string) (time.Duration, error) {
	object, err := c.client.Get(ctx, specapi.SpecChangeGVR, namespace, name)
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
	change, ok := typed.(*spec.SpecChange)
	if !ok {
		return 0, fmt.Errorf("specd: %s is not a SpecChange", name)
	}

	status := map[string]any{}
	if change.Status.Phase == "" {
		status["phase"] = specapi.PhasePending
	} else if change.Status.Phase == specapi.PhaseRunning {
		running, err := c.runningChanges(ctx, namespace, change.Spec.SystemContext)
		if err != nil {
			return 0, err
		}
		if !specsync.RunningAdmitted(change.Name, running) {
			status["phase"] = specapi.PhaseFailed
			status["message"] = "admission: another change of " + change.Spec.SystemContext + " is already running"
		}
	}
	if len(status) == 0 || specapi.StatusMatches(change.Status, status) {
		return 0, nil
	}
	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, name, status); err != nil {
		return 0, err
	}
	c.log.Info("spec change updated", "change", name, "status", status)
	return 0, nil
}

func (c *Controller) runningChanges(ctx context.Context, namespace, systemContext string) ([]string, error) {
	changes, err := c.changesFor(ctx, namespace, systemContext)
	if err != nil {
		return nil, err
	}
	running := []string{}
	for _, change := range changes {
		if change.Status.Phase == specapi.PhaseRunning {
			running = append(running, change.Name)
		}
	}
	sort.Strings(running)
	return running, nil
}

// createChange creates the one named object, so two reconciles of the same
// drift land on the same SpecChange instead of a second one.
func (c *Controller) createChange(ctx context.Context, change *spec.SpecChange) error {
	change.SetDefaults()
	if result := spec.ValidateSpecChange(change); !result.OK() {
		return fmt.Errorf("specd: %s is invalid: %w", change.Name, result.Err())
	}
	object, err := kcpclient.Unstructured(change)
	if err != nil {
		return err
	}
	if _, err := c.client.Create(ctx, object); err != nil {
		if kcpclient.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	// The status subresource drops status on a create, so the new change has no
	// phase until this write. Doing it here means no reader ever sees a
	// SpecChange without one, and the SpecChange reconcile only has to enforce
	// admission.
	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, change.Namespace, change.Name,
		map[string]any{"phase": change.Status.Phase}); err != nil {
		return err
	}
	c.log.Info("spec change created",
		"change", change.Name,
		"direction", change.Spec.Direction,
		"systemcontext", change.Spec.SystemContext,
		"fromCommit", change.Spec.FromCommit,
		"toCommit", change.Spec.ToCommit,
		"toSpecHash", change.Spec.ToSpecHash,
	)
	return nil
}
