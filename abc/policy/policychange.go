package policy

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const PolicyChangeKind = "PolicyChange"

const (
	PolicyPhaseDrafting  = "Drafting"
	PolicyPhaseTesting   = "Testing"
	PolicyPhaseEvaluated = "Evaluated"
	PolicyPhaseApplied   = "Applied"
	PolicyPhaseFailed    = "Failed"
)

const PolicyChangeListKind = "PolicyChangeList"

const (
	ConditionPolicyReady      = "PolicyReady"
	ConditionPolicyCompliant  = "PolicyCompliant"
	ConditionPolicyDenied     = "PolicyDenied"
	ConditionAcceptancePolicy = "AcceptanceOverridden"
)

const (
	ReasonPolicyTestsPassed = "PolicyTestsPassed"
	ReasonPolicyTestsFailed = "PolicyTestsFailed"
	ReasonPolicyEvaluated   = "PolicyEvaluated"
	ReasonPolicyApplied     = "PolicyApplied"
	ReasonPolicyCompliant   = "PolicyCompliant"
	ReasonPolicyViolations  = "PolicyViolations"
	ReasonPolicyDenied      = "PolicyDenied"
)

type PolicyTestResult struct {
	Passed int `json:"passed"`

	Failed int `json:"failed"`

	Output string `json:"output,omitempty"`
}

type PolicyChangeSpec struct {
	Repository string `json:"repository"`

	Branch string `json:"branch,omitempty"`

	Prompt string `json:"prompt,omitempty"`

	Requirements []string `json:"requirements,omitempty"`

	Contexts []string `json:"contexts,omitempty"`

	EnforcementAction Enforcement `json:"enforcementAction,omitempty"`

	Apply bool `json:"apply,omitempty"`
}

type PolicyChangeStatus struct {
	Phase string `json:"phase,omitempty"`

	Template string `json:"template,omitempty"`

	Constraints []string `json:"constraints,omitempty"`

	Tests *PolicyTestResult `json:"tests,omitempty"`

	Violations []Violation `json:"violations,omitempty"`

	Attempt int `json:"attempt,omitempty"`

	AgentLog string `json:"agentLog,omitempty"`

	PolicyCommit string `json:"policyCommit,omitempty"`

	Message string `json:"message,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type PolicyChange struct {
	metav1.TypeMeta `json:",inline"`

	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PolicyChangeSpec `json:"spec,omitempty"`

	Status PolicyChangeStatus `json:"status,omitempty"`
}

func ValidatePolicyChange(c *PolicyChange) error {
	if c.Name == "" {
		return fmt.Errorf("policy: metadata.name is required")
	}
	if c.Spec.Repository == "" {
		return fmt.Errorf("policy: %s names no repository", c.Name)
	}
	if c.Spec.EnforcementAction != "" && !c.Spec.EnforcementAction.Known() {
		return fmt.Errorf("policy: %s enforcementAction %q is not deny, warn or dryrun", c.Name, c.Spec.EnforcementAction)
	}
	return nil
}

func (c *PolicyChange) SetDefaults() {
	c.TypeMeta = metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: PolicyChangeKind}
	if c.Namespace == "" {
		c.Namespace = specapi.DefaultNamespace
	}
	if c.Spec.EnforcementAction == "" {
		c.Spec.EnforcementAction = EnforcementDryRun
	}
	if c.Status.Phase == "" {
		c.Status.Phase = PolicyPhaseDrafting
	}
}
