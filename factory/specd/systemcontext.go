package specd

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// reconcileSystemContext never indexes code. Ingest owns the observed facts;
// this reconcile only turns them into the three conditions, records the
// generation the status describes, and raises the spec changes the two
// directions need.
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
	if err := c.reconcileChanges(ctx, namespace, systemContext, decision); err != nil {
		return 0, err
	}
	return 0, nil
}

// reconcileChanges turns the decision into at most one change per direction.
// A change is only raised when nothing of that direction is still unfinished,
// so a code base that stays drifted does not queue work on every reconcile.
func (c *Controller) reconcileChanges(
	ctx context.Context,
	namespace string,
	systemContext *spec.SystemContext,
	decision specsync.Decision,
) error {
	fromCommit := systemContext.Status.SyncedCommit
	toCommit := systemContext.Status.ObservedCommit
	driftDue := specsync.CodeToSpecDue(decision.Drifted, fromCommit, toCommit)

	specHash, err := spec.HashSystemContextSpec(systemContext.Spec)
	if err != nil {
		return err
	}
	editDue := specsync.SpecEditDue(specHash, systemContext.Status.RealizedSpecHash)
	// A quiet context is the common case, and it costs no list to find out.
	if !driftDue && !editDue {
		return nil
	}

	changes, err := c.changesFor(ctx, namespace, systemContext.Name)
	if err != nil {
		return err
	}
	unfinished := map[string]bool{}
	for _, change := range changes {
		switch change.Status.Phase {
		case specapi.PhasePending, specapi.PhaseRunning:
			unfinished[change.Spec.Direction] = true
		}
	}

	if driftDue && !unfinished[specapi.DirectionCodeToSpec] {
		change := &spec.SpecChange{
			ObjectMeta: metav1.ObjectMeta{
				Name:      spec.ChangeNameCodeToSpec(systemContext.Name, fromCommit, toCommit),
				Namespace: namespace,
			},
			Spec: spec.SpecChangeSpec{
				SystemContext: systemContext.Name,
				Direction:     specapi.DirectionCodeToSpec,
				FromCommit:    fromCommit,
				ToCommit:      toCommit,
			},
		}
		if err := c.createChange(ctx, change); err != nil {
			return err
		}
	}

	if editDue && !unfinished[specapi.DirectionSpecToCode] {
		change := &spec.SpecChange{
			ObjectMeta: metav1.ObjectMeta{
				Name:      spec.ChangeNameSpecToCode(systemContext.Name, specHash),
				Namespace: namespace,
			},
			Spec: spec.SpecChangeSpec{
				SystemContext: systemContext.Name,
				Direction:     specapi.DirectionSpecToCode,
				FromSpecHash:  systemContext.Status.RealizedSpecHash,
				ToSpecHash:    specHash,
			},
		}
		if err := c.createChange(ctx, change); err != nil {
			return err
		}
	}
	return nil
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
