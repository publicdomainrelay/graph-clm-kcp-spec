package specd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

// specGate evaluates a SpecToCode batch against the ArchitectureModel built
// from the post-delta specs alone, before anything is realized. It returns nil
// when no policy library is configured.
func (c *Controller) specGate(ctx context.Context, namespace string, repository *spec.Repository, members []*spec.SpecChange) (*policyeval.SpecResult, error) {
	if c.opts.PolicyLibrary == "" {
		return nil, nil
	}
	library, err := policyeval.Load(c.opts.PolicyLibrary)
	if err != nil {
		return nil, fmt.Errorf("specd: read the policy library %s: %w", c.opts.PolicyLibrary, err)
	}
	if len(library.Templates) == 0 {
		return nil, nil
	}
	contexts, err := c.contextObjects(ctx, namespace)
	if err != nil {
		return nil, err
	}
	applied := map[string]spec.SystemContextSpec{}
	ordered := []spec.SystemContext{}
	for _, context := range contexts {
		if context.Spec.Repository != repository.Name {
			continue
		}
		ordered = append(ordered, context)
	}
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].Name < ordered[right].Name })
	for _, member := range members {
		context, ok := contexts[member.Spec.SystemContext]
		if !ok {
			continue
		}
		changeDelta, err := c.deltaForChange(ctx, namespace, member)
		if err != nil {
			return nil, err
		}
		applied[member.Spec.SystemContext] = delta.Apply(realizedSpecOf(&context), changeDelta)
	}
	result, err := policyeval.CheckSpecs(ctx, policyeval.SpecInput{
		Repository: repository.Name,
		Contexts:   ordered,
		Applied:    applied,
		Binding:    library.Manifest.Binding(),
		Library:    library,
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// recordPolicyDenied sends a batch back to drafting with the policy messages
// and the reason, and realizes nothing.
func (c *Controller) recordPolicyDenied(ctx context.Context, namespace string, members []*spec.SpecChange, result *policyeval.SpecResult) {
	messages := result.Messages()
	message := "policy denied at spec time: " + strings.Join(messages, "; ")
	for _, member := range members {
		conditions := condition.Copy(member.Status.Conditions)
		condition.SetFalse(&conditions, member.GetGeneration(), specapi.ConditionPolicyValid,
			specapi.ReasonPolicyDeniedAtSpec, message)
		if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, member.Name, map[string]any{
			"phase":      specapi.PhaseFailed,
			"message":    tailMessage(message),
			"conditions": conditions,
		}); err != nil {
			c.log.Error("could not record the policy denial", "change", member.Name, "err", err)
			continue
		}
		c.enqueueContext(ctx, namespace, member.Spec.SystemContext)
	}
	c.log.Warn("the spec was denied by policy before realize",
		"changes", len(members), "violations", len(result.Decision.Denied), "message", message)
}

// recordPolicyGateError fails a batch whose policy library could not be read: a
// configured gate that cannot run must not silently pass.
func (c *Controller) recordPolicyGateError(ctx context.Context, namespace string, members []*spec.SpecChange, failure error) {
	message := "policy gate: " + failure.Error()
	for _, member := range members {
		conditions := condition.Copy(member.Status.Conditions)
		condition.SetFalse(&conditions, member.GetGeneration(), specapi.ConditionPolicyValid,
			specapi.ReasonPolicyGateError, message)
		if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, member.Name, map[string]any{
			"phase":      specapi.PhaseFailed,
			"message":    tailMessage(message),
			"conditions": conditions,
		}); err != nil {
			c.log.Error("could not record the policy gate error", "change", member.Name, "err", err)
			continue
		}
		c.enqueueContext(ctx, namespace, member.Spec.SystemContext)
	}
	c.log.Error("the policy gate could not run", "changes", len(members), "err", failure)
}

// clearPolicyDenial marks a change the gate now accepts, so a fixed spec does
// not keep the False condition it earned before.
func (c *Controller) clearPolicyDenial(ctx context.Context, namespace string, members []*spec.SpecChange) {
	for _, member := range members {
		if !hasPolicyDenial(member.Status.Conditions) {
			continue
		}
		conditions := condition.Copy(member.Status.Conditions)
		condition.SetTrue(&conditions, member.GetGeneration(), specapi.ConditionPolicyValid,
			specapi.ReasonPolicyAllowed, "the declared state passes the policy library")
		if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, member.Name, map[string]any{
			"conditions": conditions,
		}); err != nil {
			c.log.Error("could not clear the policy denial", "change", member.Name, "err", err)
		}
	}
}

func hasPolicyDenial(conditions []metav1.Condition) bool {
	for _, existing := range conditions {
		if existing.Type == specapi.ConditionPolicyValid && existing.Status != "True" {
			return true
		}
	}
	return false
}
