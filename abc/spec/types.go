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

// AgentSpec selects the agent of one Repository. Kind is the same option
// string specd takes (claude, scripted:<file>, later pi); Command and Args
// override the model command of the claude kind.
type AgentSpec struct {
	Kind    string   `json:"kind,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

// GitSource is a remote working tree: where to clone from and which ref to
// check out. An empty ref is the remote's default branch.
type GitSource struct {
	URL string `json:"url"`
	Ref string `json:"ref,omitempty"`
}

// RepositorySource is where the working tree under management comes from. A
// path is used as it is; a git url is cloned into the controller's cache and
// kept up to date there. It is the phase 7 trigger: one manifest names either
// kind of source and the controller populates it.
type RepositorySource struct {
	Path string     `json:"path,omitempty"`
	Git  *GitSource `json:"git,omitempty"`
}

// RepositoryPopulate is what the controller does with a working tree once it
// has one: which files to index, how to split them into SystemContexts, and
// whether to send a model over each new context.
type RepositoryPopulate struct {
	Partition string     `json:"partition,omitempty"`
	Include   []string   `json:"include,omitempty"`
	Exclude   []string   `json:"exclude,omitempty"`
	Summarize bool       `json:"summarize,omitempty"`
	Agent     *AgentSpec `json:"agent,omitempty"`
}

const (
	// PartitionDirectory is one context per directory that holds a source file.
	PartitionDirectory = "directory"
	// PartitionPackage is one context per package or module root.
	PartitionPackage = "package"
)

type RepositorySpec struct {
	Path   string            `json:"path,omitempty"`
	Source *RepositorySource `json:"source,omitempty"`
	Branch string            `json:"branch,omitempty"`
	Verify []string          `json:"verify,omitempty"`
	Agent  *AgentSpec        `json:"agent,omitempty"`

	Populate *RepositoryPopulate `json:"populate,omitempty"`
}

// PopulateCounts is the tally Repository.status.contexts carries: how many
// SystemContexts the repository has, how many of them a model has summarized,
// and how many of the rest could not be summarized at all.
type PopulateCounts struct {
	Total      int `json:"total"`
	Summarized int `json:"summarized"`
	Failed     int `json:"failed"`
}

type RepositoryStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// ResolvedPath is the working tree the controller resolved the source to:
	// spec.path for a path source, a directory under --cache-dir for a git one.
	// Everything that needs to read or write the tree reads it through
	// Repository.WorkPath.
	ResolvedPath  string `json:"resolvedPath,omitempty"`
	HeadCommit    string `json:"headCommit,omitempty"`
	IndexedCommit string `json:"indexedCommit,omitempty"`
	// Phase runs Cloning -> Indexing -> Populating -> Populated, or Failed.
	Phase    string          `json:"phase,omitempty"`
	Contexts *PopulateCounts `json:"contexts,omitempty"`
	// PopulateRequest is the specs.publicdomainrelay.dev/populate-request
	// annotation value this status answers, so a caller can ask for a re-index
	// and wait for it to have happened.
	PopulateRequest string             `json:"populateRequest,omitempty"`
	Conditions      []metav1.Condition `json:"conditions,omitempty"`
}

// Source is the declared source, with the phase 1 path field read as a path
// source so an older manifest keeps working.
func (r *Repository) Source() RepositorySource {
	if r.Spec.Source != nil {
		return *r.Spec.Source
	}
	return RepositorySource{Path: r.Spec.Path}
}

// WorkPath is the working tree the controller resolved the source to. A reader
// that needs the tree itself asks for this, never for spec.path: a git source
// has no spec.path at all.
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

// Partition is the partition mode of the populate step, defaulted to the
// directory partition so an older manifest means what it always meant.
func (r *Repository) Partition() string {
	if r.Spec.Populate != nil && r.Spec.Populate.Partition != "" {
		return r.Spec.Populate.Partition
	}
	return PartitionDirectory
}

func (r *Repository) Summarize() bool {
	return r.Spec.Populate != nil && r.Spec.Populate.Summarize
}

// PopulateAgent is the agent the populate step uses. It is separate from
// spec.agent, which selects the agent of a spec -> code realize.
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
	// ArchKindNode marks a SystemContext that is one node of an arch.yaml.
	ArchKindNode = "node"
	// ArchKindDocument marks the one SystemContext that carries the arch.yaml
	// top-level header and the order of its sections.
	ArchKindDocument = "document"
)

// ArchSpec is the open architecture (arch.yaml) view of a SystemContext. Every
// node of an imported arch.yaml becomes one SystemContext; this block carries
// the id the document used, where the node sat in the tree, and the node body
// with its inline children replaced by their id refs, so export can put the
// document back together. The document object (Kind document) carries the
// top-level header instead of a node body.
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

// ArchSection is one top-level section of the document, in document order: the
// key and whether it is a map (id is the key) or a list of nodes.
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
	Files       []string            `json:"files,omitempty"`
	Interfaces  []ObservedInterface `json:"interfaces,omitempty"`
	Fingerprint string              `json:"fingerprint,omitempty"`
}

// SystemContextStatus is the observed state. ObservedCommit and Observed are
// what the code is right now. SyncedCommit and SyncedFingerprint are the
// baseline the spec was last brought into agreement with: they move only when
// an ingest, a realize or a human edit acknowledges the code, so Drifted stays
// true until the drift is worked off.
type SystemContextStatus struct {
	ObservedGeneration int64         `json:"observedGeneration,omitempty"`
	ObservedCommit     string        `json:"observedCommit,omitempty"`
	Observed           ObservedFacts `json:"observed,omitempty"`
	SyncedCommit       string        `json:"syncedCommit,omitempty"`
	SyncedFingerprint  string        `json:"syncedFingerprint,omitempty"`
	// SyncedObserved is the facts the synced baseline was taken from. Observed
	// is overwritten by every ingest, so the code -> spec delta needs the old
	// side kept: this is that side.
	SyncedObserved   ObservedFacts `json:"syncedObserved,omitempty"`
	RealizedSpecHash string        `json:"realizedSpecHash,omitempty"`
	// RealizedSpec is the spec the hash above was taken from: the last spec
	// that was realized or absorbed. It is the old side of a spec -> code
	// delta, which is therefore computable from kcp alone.
	RealizedSpec *SystemContextSpec `json:"realizedSpec,omitempty"`
	Conditions   []metav1.Condition `json:"conditions,omitempty"`
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

	// Delta is what the change asks for, never the whole spec: the spec edit
	// for a SpecToCode change, the observed fact change for a CodeToSpec one.
	Delta *Delta `json:"delta,omitempty"`
}

type SpecChangeStatus struct {
	Phase string `json:"phase,omitempty"`

	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`

	VerifyExitCode int      `json:"verifyExitCode,omitempty"`
	FilesTouched   []string `json:"filesTouched,omitempty"`
	AgentLog       string   `json:"agentLog,omitempty"`
	Message        string   `json:"message,omitempty"`
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
