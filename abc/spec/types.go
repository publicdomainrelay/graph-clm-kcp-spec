package spec

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

type Level string

const (
	LevelMust   Level = "MUST"
	LevelShould Level = "SHOULD"
	LevelMay    Level = "MAY"
)

func Levels() []Level {
	return []Level{LevelMust, LevelShould, LevelMay}
}

type AgentSpec struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type RepositorySpec struct {
	Path   string     `json:"path"`
	Branch string     `json:"branch,omitempty"`
	Verify []string   `json:"verify,omitempty"`
	Agent  *AgentSpec `json:"agent,omitempty"`
}

type RepositoryStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	HeadCommit         string             `json:"headCommit,omitempty"`
	IndexedCommit      string             `json:"indexedCommit,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type Repository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RepositorySpec   `json:"spec,omitempty"`
	Status RepositoryStatus `json:"status,omitempty"`
}

type Requirement struct {
	ID       string   `json:"id"`
	Level    Level    `json:"level"`
	Text     string   `json:"text"`
	CodeRefs []string `json:"codeRefs,omitempty"`
}

type Interface struct {
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	Signature string `json:"signature,omitempty"`
	File      string `json:"file,omitempty"`
}

type SystemContextSpec struct {
	Repository   string        `json:"repository"`
	Upstream     string        `json:"upstream,omitempty"`
	Overlay      []string      `json:"overlay,omitempty"`
	Orchestrator string        `json:"orchestrator,omitempty"`
	Intent       string        `json:"intent,omitempty"`
	Requirements []Requirement `json:"requirements,omitempty"`
	Interfaces   []Interface   `json:"interfaces,omitempty"`
	CodeRefs     []string      `json:"codeRefs,omitempty"`
}

type ObservedInterface struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Signature   string `json:"signature,omitempty"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
	CodegraphID string `json:"codegraphId,omitempty"`
}

type ObservedFacts struct {
	Files       []string            `json:"files,omitempty"`
	Interfaces  []ObservedInterface `json:"interfaces,omitempty"`
	Fingerprint string              `json:"fingerprint,omitempty"`
}

type SystemContextStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	ObservedCommit     string             `json:"observedCommit,omitempty"`
	Observed           ObservedFacts      `json:"observed,omitempty"`
	RealizedSpecHash   string             `json:"realizedSpecHash,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type SystemContext struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SystemContextSpec   `json:"spec,omitempty"`
	Status SystemContextStatus `json:"status,omitempty"`
}

type SpecChangeSpec struct {
	SystemContext string `json:"systemContext"`

	Direction string `json:"direction"`

	FromSpecHash string `json:"fromSpecHash,omitempty"`
	ToSpecHash   string `json:"toSpecHash,omitempty"`

	FromCommit string `json:"fromCommit,omitempty"`
	ToCommit   string `json:"toCommit,omitempty"`
}

type SpecChangeStatus struct {
	Phase string `json:"phase,omitempty"`

	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`

	VerifyExitCode int    `json:"verifyExitCode,omitempty"`
	AgentLog       string `json:"agentLog,omitempty"`
	Message        string `json:"message,omitempty"`
}

type SpecChange struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SpecChangeSpec   `json:"spec,omitempty"`
	Status SpecChangeStatus `json:"status,omitempty"`
}

func (r *Repository) SetDefaults() {
	r.TypeMeta = metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.RepositoryKind}
	if r.Namespace == "" {
		r.Namespace = specapi.DefaultNamespace
	}
	if r.Spec.Branch == "" {
		r.Spec.Branch = "main"
	}
}

func (s *SystemContext) SetDefaults() {
	s.TypeMeta = metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.SystemContextKind}
	if s.Namespace == "" {
		s.Namespace = specapi.DefaultNamespace
	}
	if s.Spec.Upstream == "" {
		s.Spec.Upstream = RefSelf
	}
}

func (c *SpecChange) SetDefaults() {
	c.TypeMeta = metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.SpecChangeKind}
	if c.Namespace == "" {
		c.Namespace = specapi.DefaultNamespace
	}
	if c.Status.Phase == "" {
		c.Status.Phase = specapi.PhasePending
	}
}
