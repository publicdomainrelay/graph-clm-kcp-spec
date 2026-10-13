package spec

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

// PlanEdit is one staged spec edit of a plan item: an upsert by id of the
// requirements, interfaces and interactions it names, the removal of the ids
// it lists, and the intent when it is set.
type PlanEdit struct {
	Context string `json:"context"`

	Intent string `json:"intent,omitempty"`

	Requirements []Requirement `json:"requirements,omitempty"`

	RemoveRequirements []string `json:"removeRequirements,omitempty"`

	Interfaces []Interface `json:"interfaces,omitempty"`

	RemoveInterfaces []string `json:"removeInterfaces,omitempty"`

	Interactions []Interaction `json:"interactions,omitempty"`

	RemoveInteractions []string `json:"removeInteractions,omitempty"`
}

type PlanItem struct {
	ID string `json:"id"`

	Summary string `json:"summary,omitempty"`

	// Repository names the repository whose instance acts on the item. Empty
	// means the plan's own repository.
	Repository string `json:"repository,omitempty"`

	DependsOn []string `json:"dependsOn,omitempty"`

	Findings []string `json:"findings,omitempty"`

	Edits []PlanEdit `json:"edits,omitempty"`

	Acceptance []AcceptanceStep `json:"acceptance,omitempty"`
}

type PlanSpec struct {
	Repository string `json:"repository"`

	Title string `json:"title,omitempty"`

	Objective string `json:"objective,omitempty"`

	Approved bool `json:"approved,omitempty"`

	ApprovedBy string `json:"approvedBy,omitempty"`

	Items []PlanItem `json:"items,omitempty"`
}

type PlanItemStatus struct {
	ID string `json:"id"`

	Phase string `json:"phase,omitempty"`

	// AppliedSpecHashes is the spec hash of each context right after the item's
	// edit landed: the SpecChange whose toSpecHash matches realizes it.
	AppliedSpecHashes map[string]string `json:"appliedSpecHashes,omitempty"`

	Changes []string `json:"changes,omitempty"`

	Preview []string `json:"preview,omitempty"`

	Commit string `json:"commit,omitempty"`

	// AcceptanceHash is the hash of the acceptance steps the recorded results
	// came from: an item whose steps change runs its acceptance again.
	AcceptanceHash string `json:"acceptanceHash,omitempty"`

	Acceptance []AcceptanceResult `json:"acceptance,omitempty"`

	Message string `json:"message,omitempty"`
}

type PlanStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	Phase string `json:"phase,omitempty"`

	Items []PlanItemStatus `json:"items,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type Plan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlanSpec   `json:"spec,omitempty"`
	Status PlanStatus `json:"status,omitempty"`
}

func (p *Plan) SetDefaults() {
	p.TypeMeta = metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.PlanKind}
	if p.Namespace == "" {
		p.Namespace = specapi.DefaultNamespace
	}
}

func (p *Plan) ItemStatus(id string) (PlanItemStatus, bool) {
	for _, item := range p.Status.Items {
		if item.ID == id {
			return item, true
		}
	}
	return PlanItemStatus{ID: id}, false
}

type FindingSpec struct {
	Repository string `json:"repository,omitempty"`

	Source string `json:"source"`

	Ref string `json:"ref,omitempty"`

	Context string `json:"context,omitempty"`

	Unit string `json:"unit,omitempty"`

	Summary string `json:"summary"`

	Severity string `json:"severity,omitempty"`
}

type FindingStatus struct {
	State string `json:"state,omitempty"`

	Plan string `json:"plan,omitempty"`

	Item string `json:"item,omitempty"`

	ResolvedCommit string `json:"resolvedCommit,omitempty"`

	Message string `json:"message,omitempty"`
}

type Finding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FindingSpec   `json:"spec,omitempty"`
	Status FindingStatus `json:"status,omitempty"`
}

func (f *Finding) SetDefaults() {
	f.TypeMeta = metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.FindingKind}
	if f.Namespace == "" {
		f.Namespace = specapi.DefaultNamespace
	}
}

type AgentRunSpec struct {
	Purpose string `json:"purpose"`

	Change string `json:"change,omitempty"`

	Context string `json:"context,omitempty"`

	Agent string `json:"agent,omitempty"`

	Command string `json:"command,omitempty"`
}

type AgentRunStatus struct {
	Phase string `json:"phase,omitempty"`

	StartedAt string `json:"startedAt,omitempty"`

	FinishedAt string `json:"finishedAt,omitempty"`

	DurationSeconds float64 `json:"durationSeconds,omitempty"`

	Message string `json:"message,omitempty"`
}

type AgentRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentRunSpec   `json:"spec,omitempty"`
	Status AgentRunStatus `json:"status,omitempty"`
}

func (r *AgentRun) SetDefaults() {
	r.TypeMeta = metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.AgentRunKind}
	if r.Namespace == "" {
		r.Namespace = specapi.DefaultNamespace
	}
}
