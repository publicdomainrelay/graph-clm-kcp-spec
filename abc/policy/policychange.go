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

	Slug string `json:"slug,omitempty"`

	Prompt string `json:"prompt,omitempty"`

	Requirements []string `json:"requirements,omitempty"`

	Contexts []string `json:"contexts,omitempty"`

	Pack string `json:"pack,omitempty"`

	PackVersion string `json:"packVersion,omitempty"`

	EnforcementAction Enforcement `json:"enforcementAction,omitempty"`

	Apply bool `json:"apply,omitempty"`
}

func (s PolicyChangeSpec) Mode() string {
	if s.Pack != "" {
		return GenerateModeBind
	}
	return GenerateModePolicy
}

func (s PolicyChangeSpec) TemplateSlug() string {
	if s.Slug != "" {
		return s.Slug
	}
	return s.Repository
}

type PolicyChangeStatus struct {
	Phase string `json:"phase,omitempty"`

	Mode string `json:"mode,omitempty"`

	Slug string `json:"slug,omitempty"`

	Template string `json:"template,omitempty"`

	Constraints []string `json:"constraints,omitempty"`

	Tests *PolicyTestResult `json:"tests,omitempty"`

	Checks []Check `json:"checks,omitempty"`

	Binding *Binding `json:"binding,omitempty"`

	Violations []CompactViolation `json:"violations,omitempty"`

	Attempt int `json:"attempt,omitempty"`

	AgentLog string `json:"agentLog,omitempty"`

	PolicyCommit string `json:"policyCommit,omitempty"`

	Message string `json:"message,omitempty"`

	Owner string `json:"owner,omitempty"`

	OwnerPid int `json:"ownerPid,omitempty"`

	OwnerStartedAt string `json:"ownerStartedAt,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type CompactViolation struct {
	Policy string `json:"policy,omitempty"`

	Constraint string `json:"constraint,omitempty"`

	Enforcement Enforcement `json:"enforcement,omitempty"`

	Severity Severity `json:"severity,omitempty"`

	Msg string `json:"msg,omitempty"`

	Object string `json:"object,omitempty"`

	File string `json:"file,omitempty"`

	Line int `json:"line,omitempty"`
}

func CompactViolationOf(violation Violation) CompactViolation {
	out := CompactViolation{
		Policy:      violation.Policy,
		Constraint:  violation.Constraint,
		Enforcement: violation.Enforcement,
		Severity:    violation.Severity,
		Msg:         violation.Msg,
		Object:      violation.Object.String(),
	}
	if violation.Location != nil {
		out.File = violation.Location.File
		out.Line = violation.Location.Line
	}
	return out
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
