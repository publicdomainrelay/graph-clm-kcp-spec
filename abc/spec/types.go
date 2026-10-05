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
	Kind    string   `json:"kind,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type GitSource struct {
	URL string `json:"url"`
	Ref string `json:"ref,omitempty"`
}

type RepositorySource struct {
	Path string     `json:"path,omitempty"`
	Git  *GitSource `json:"git,omitempty"`
}

type RepositoryPopulate struct {
	Partition string     `json:"partition,omitempty"`
	Include   []string   `json:"include,omitempty"`
	Exclude   []string   `json:"exclude,omitempty"`
	Summarize bool       `json:"summarize,omitempty"`
	Arch      string     `json:"arch,omitempty"`
	Root      bool       `json:"root,omitempty"`
	Agent     *AgentSpec `json:"agent,omitempty"`
}

const (
	PartitionDirectory = "directory"
	PartitionPackage   = "package"

	DefaultArchPath = ".tools/open-architecture/arch.yaml"
)

type AcceptanceStep struct {
	Name string `json:"name"`

	Command []string `json:"command"`

	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`

	Gate bool `json:"gate,omitempty"`

	Env map[string]string `json:"env,omitempty"`
}

// AcceptanceOverride lets an operator land one commit past a red gating
// acceptance step. It is one shot: specd removes the entry it consumed after
// the commit lands, so the next realization is gated again.
type AcceptanceOverride struct {
	Step string `json:"step"`

	Reason string `json:"reason"`

	By string `json:"by,omitempty"`

	At string `json:"at,omitempty"`
}

type RepositorySpec struct {
	Path   string            `json:"path,omitempty"`
	Source *RepositorySource `json:"source,omitempty"`
	Branch string            `json:"branch,omitempty"`
	Verify []string          `json:"verify,omitempty"`
	Agent  *AgentSpec        `json:"agent,omitempty"`

	Acceptance []AcceptanceStep `json:"acceptance,omitempty"`

	AcceptanceOverrides []AcceptanceOverride `json:"acceptanceOverrides,omitempty"`

	Populate *RepositoryPopulate `json:"populate,omitempty"`
}

type PopulateCounts struct {
	Total      int `json:"total"`
	Summarized int `json:"summarized"`
	Failed     int `json:"failed"`
}

type RepositoryStatus struct {
	ObservedGeneration int64                   `json:"observedGeneration,omitempty"`
	ResolvedPath       string                  `json:"resolvedPath,omitempty"`
	HeadCommit         string                  `json:"headCommit,omitempty"`
	IndexedCommit      string                  `json:"indexedCommit,omitempty"`
	Phase              string                  `json:"phase,omitempty"`
	Contexts           *PopulateCounts         `json:"contexts,omitempty"`
	PopulateRequest    string                  `json:"populateRequest,omitempty"`
	OpenArchitecture   *OpenArchitectureStatus `json:"openArchitecture,omitempty"`
	Conditions         []metav1.Condition      `json:"conditions,omitempty"`
}

type OpenArchitectureStatus struct {
	Branch    string   `json:"branch,omitempty"`
	Commit    string   `json:"commit,omitempty"`
	Pushed    string   `json:"pushed,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
}

func (r *Repository) Source() RepositorySource {
	if r.Spec.Source != nil {
		return *r.Spec.Source
	}
	return RepositorySource{Path: r.Spec.Path}
}

func (r *Repository) WorkPath() string {
	if r.Status.ResolvedPath != "" {
		return r.Status.ResolvedPath
	}
	if r.Spec.Path != "" {
		return r.Spec.Path
	}
	if r.Spec.Source != nil {
		return r.Spec.Source.Path
	}
	return ""
}

func (r *Repository) Partition() string {
	if r.Spec.Populate != nil && r.Spec.Populate.Partition != "" {
		return r.Spec.Populate.Partition
	}
	return PartitionDirectory
}

func (r *Repository) Summarize() bool {
	return r.Spec.Populate != nil && r.Spec.Populate.Summarize
}

func (r *Repository) RootContext() bool {
	return r.Spec.Populate != nil && r.Spec.Populate.Root
}

func (r *Repository) ArchPath() string {
	if r.Spec.Populate != nil {
		return r.Spec.Populate.Arch
	}
	return ""
}

func (r *Repository) PopulateAgent() *AgentSpec {
	if r.Spec.Populate != nil {
		return r.Spec.Populate.Agent
	}
	return nil
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

const (
	InitiatorSelf = "self"

	InitiatorPeer = "peer"

	DefaultInteractionLevel Level = LevelShould
)

func Initiators() []string {
	return []string{InitiatorSelf, InitiatorPeer}
}

// Interaction is one declared flow between this context and a peer, keyed by
// id. It is the spec-level fact a portable policy reads, so a decision about
// who talks to whom is checkable before any code exists. A flow marked
// forbidden is a declared "must never": it is kept in the model as a
// declared-forbidden entry and a matching declared or observed flow is a
// violation.
type Interaction struct {
	ID string `json:"id"`

	Peer string `json:"peer"`

	Initiator string `json:"initiator"`

	Channel string `json:"channel,omitempty"`

	Carries []string `json:"carries,omitempty"`

	Purpose string `json:"purpose,omitempty"`

	Level Level `json:"level,omitempty"`

	Forbidden bool `json:"forbidden,omitempty"`
}

const (
	ArchKindNode     = "node"
	ArchKindDocument = "document"
)

type ArchSpec struct {
	ID       string `json:"id"`
	Kind     string `json:"kind,omitempty"`
	Section  string `json:"section,omitempty"`
	Form     string `json:"form,omitempty"`
	Position int    `json:"position,omitempty"`

	Parent string `json:"parent,omitempty"`
	Slot   string `json:"slot,omitempty"`

	Upstream     string   `json:"upstream,omitempty"`
	Overlay      []string `json:"overlay,omitempty"`
	Orchestrator string   `json:"orchestrator,omitempty"`
	DependsOn    []string `json:"dependsOn,omitempty"`
	Introduces   []string `json:"introduces,omitempty"`
	Code         []string `json:"code,omitempty"`

	Sections []ArchSection `json:"sections,omitempty"`

	Node     map[string]any `json:"node,omitempty"`
	Document map[string]any `json:"document,omitempty"`
}

type ArchSection struct {
	Key  string `json:"key"`
	Form string `json:"form,omitempty"`
}

type SystemContextSpec struct {
	Repository   string        `json:"repository"`
	Upstream     string        `json:"upstream,omitempty"`
	Overlay      []string      `json:"overlay,omitempty"`
	Orchestrator string        `json:"orchestrator,omitempty"`
	DependsOn    []string      `json:"dependsOn,omitempty"`
	Introduces   []string      `json:"introduces,omitempty"`
	Intent       string        `json:"intent,omitempty"`
	Requirements []Requirement `json:"requirements,omitempty"`
	Interfaces   []Interface   `json:"interfaces,omitempty"`
	Interactions []Interaction `json:"interactions,omitempty"`
	CodeRefs     []string      `json:"codeRefs,omitempty"`
	Arch         *ArchSpec     `json:"arch,omitempty"`
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
	Files      []string            `json:"files,omitempty"`
	TreeFiles  []string            `json:"treeFiles,omitempty"`
	Interfaces []ObservedInterface `json:"interfaces,omitempty"`

	Fingerprint string `json:"fingerprint,omitempty"`
}

type SystemContextStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	ObservedCommit     string             `json:"observedCommit,omitempty"`
	Observed           ObservedFacts      `json:"observed,omitempty"`
	SyncedCommit       string             `json:"syncedCommit,omitempty"`
	SyncedFingerprint  string             `json:"syncedFingerprint,omitempty"`
	SyncedObserved     ObservedFacts      `json:"syncedObserved,omitempty"`
	RealizedSpecHash   string             `json:"realizedSpecHash,omitempty"`
	RealizedSpec       *SystemContextSpec `json:"realizedSpec,omitempty"`
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

	Delta *Delta `json:"delta,omitempty"`
}

type AcceptanceResult struct {
	Name string `json:"name"`

	ExitCode int `json:"exitCode"`

	DurationSeconds float64 `json:"durationSeconds"`

	Passed bool `json:"passed"`

	OutputTail string `json:"outputTail,omitempty"`

	Overridden bool `json:"overridden,omitempty"`

	OverrideBy string `json:"overrideBy,omitempty"`

	OverrideReason string `json:"overrideReason,omitempty"`
}

type SpecChangeStatus struct {
	Phase string `json:"phase,omitempty"`

	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`

	VerifyExitCode int      `json:"verifyExitCode,omitempty"`
	FilesTouched   []string `json:"filesTouched,omitempty"`
	AgentLog       string   `json:"agentLog,omitempty"`
	Message        string   `json:"message,omitempty"`

	Acceptance []AcceptanceResult `json:"acceptance,omitempty"`

	Progress []ProgressRecord `json:"progress,omitempty"`

	RequirementCoverage []RequirementVerdict `json:"requirementCoverage,omitempty"`

	Attempt int `json:"attempt,omitempty"`

	RetryReason string `json:"retryReason,omitempty"`

	RetryBy string `json:"retryBy,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// RequirementVerdict is what the coverage judgment found for one requirement
// the change added or changed: whether the realized diff implements it, and the
// evidence the model named.
type RequirementVerdict struct {
	ID string `json:"id"`

	Implemented bool `json:"implemented"`

	Evidence string `json:"evidence,omitempty"`
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
	if r.Spec.Populate != nil && r.Spec.Populate.Partition == "" {
		r.Spec.Populate.Partition = PartitionDirectory
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
