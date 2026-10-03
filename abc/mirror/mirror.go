// Package mirror is the pure half of the git-native spec mirror: one YAML
// document per SystemContext under `.specs/` in the managed repository, so a
// pull request carries the spec and the code together.
//
// The document is the spec and nothing else: no status, no conditions, no
// runtime metadata. Its key order comes from the Go struct field order, so the
// same spec always renders to the same bytes and a pull that changed nothing
// leaves the file alone.
//
// Sync semantics are decided here, with no I/O, so the conflict rule is a unit
// test: a file and kcp are only in conflict when both moved since the last sync
// recorded in the `synced-hash` annotation.
package mirror

import (
	"errors"
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

// Dir is where the mirror lives inside the managed repository.
const Dir = ".specs"

// ErrConflict is both sides of one context having moved since the last sync.
var ErrConflict = errors.New("mirror: kcp and the file both changed since the last sync")

type Direction string

const (
	Pull Direction = "pull"
	Push Direction = "push"
	Both Direction = "both"
)

func ParseDirection(value string) (Direction, error) {
	switch Direction(value) {
	case Pull, Push, Both:
		return Direction(value), nil
	}
	return "", fmt.Errorf("mirror: %q is not pull, push or both", value)
}

type Prefer string

const (
	PreferNone Prefer = ""
	PreferKCP  Prefer = "kcp"
	PreferGit  Prefer = "git"
)

func ParsePrefer(value string) (Prefer, error) {
	switch Prefer(value) {
	case PreferNone, PreferKCP, PreferGit:
		return Prefer(value), nil
	}
	return "", fmt.Errorf("mirror: %q is not kcp or git", value)
}

type Metadata struct {
	Name string `json:"name"`

	Namespace string `json:"namespace,omitempty"`
}

// Document is one `.specs/<name>.yaml`.
type Document struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata Metadata `json:"metadata"`

	Spec spec.SystemContextSpec `json:"spec"`
}

// Render writes one context's spec as the mirror document. The spec is
// canonicalized first, so the keyed lists come out in their key order and a
// reordered manifest does not change the file. The code refs ingest derived
// from the tree are left out: they are an observation, not a decision, and
// including them would rewrite every file on every commit and drown a pull
// request in spec churn.
func Render(name, namespace string, contextSpec spec.SystemContextSpec) ([]byte, error) {
	declared := spec.Canonicalize(contextSpec)
	declared.CodeRefs = nil
	document := Document{
		APIVersion: specapi.Group + "/" + specapi.Version,
		Kind:       specapi.SystemContextKind,
		Metadata:   Metadata{Name: name, Namespace: namespace},
		Spec:       declared,
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("mirror: render %s: %w", name, err)
	}
	return data, nil
}

// Parse reads one mirror document and validates it as a spec a human could
// have applied to kcp.
func Parse(name string, data []byte) (spec.SystemContext, error) {
	document := Document{}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return spec.SystemContext{}, fmt.Errorf("mirror: parse %s: %w", name, err)
	}
	if document.Kind != "" && document.Kind != specapi.SystemContextKind {
		return spec.SystemContext{}, fmt.Errorf("mirror: %s is a %s, want %s", name, document.Kind, specapi.SystemContextKind)
	}
	if document.Metadata.Name != "" && document.Metadata.Name != name {
		return spec.SystemContext{}, fmt.Errorf("mirror: %s names the context %q", name, document.Metadata.Name)
	}
	if want := specapi.Group + "/" + specapi.Version; document.APIVersion != "" && document.APIVersion != want {
		return spec.SystemContext{}, fmt.Errorf("mirror: %s has apiVersion %q, want %q", name, document.APIVersion, want)
	}
	out := spec.SystemContext{}
	out.Name = name
	out.Namespace = document.Metadata.Namespace
	out.Spec = document.Spec
	if result := spec.ValidateSystemContext(&out); !result.OK() {
		return spec.SystemContext{}, fmt.Errorf("mirror: %s does not validate: %v", name, result.Err())
	}
	return out, nil
}

// DeclaredHash hashes the state a human owns. The code refs ingest derives
// from the tree are left out, so a re-index is not read as a spec change on
// either side of the mirror.
func DeclaredHash(contextSpec spec.SystemContextSpec) (string, error) {
	contextSpec.CodeRefs = nil
	return spec.HashSystemContextSpec(contextSpec)
}

// Apply merges a file's spec into the live spec. The fields a human owns come
// from the file; the fields the tool owns stay: the code refs ingest derived,
// and the repository, which is what the context belongs to and not something a
// mirror may move.
func Apply(live, file spec.SystemContextSpec) (spec.SystemContextSpec, error) {
	if file.Repository != "" && live.Repository != "" && file.Repository != live.Repository {
		return live, fmt.Errorf("mirror: the file names repository %q but kcp has %q; a mirror does not move a context between repositories", file.Repository, live.Repository)
	}
	out := live
	if live.Repository == "" {
		out.Repository = file.Repository
	}
	out.Upstream = file.Upstream
	out.Overlay = file.Overlay
	out.Orchestrator = file.Orchestrator
	out.DependsOn = file.DependsOn
	out.Introduces = file.Introduces
	out.Intent = file.Intent
	out.Requirements = file.Requirements
	out.Interfaces = file.Interfaces
	out.Arch = file.Arch
	return out, nil
}

// State is one context's three hashes: the last sync the annotation recorded,
// what kcp holds now, and what the file holds now. Last is empty when the
// annotation is absent; HasFile says whether a file exists at all.
type State struct {
	HasFile bool

	Last string

	Kcp string

	File string
}

// Action is what one direction does with one context.
type Action struct {
	Pull bool

	Push bool

	Note string
}

// Resolve decides one context. The rule is the plan's: refuse only when both
// sides moved since the last sync, unless the caller says which side wins. A
// side that did not move is never overwritten by the other, and a push never
// clobbers a kcp that moved.
func Resolve(direction Direction, prefer Prefer, state State) (Action, error) {
	wantPull := direction == Pull || direction == Both
	wantPush := direction == Push || direction == Both
	if !state.HasFile {
		return Action{Pull: wantPull, Note: "no file yet"}, nil
	}
	if state.Kcp != state.File && state.Last != "" && state.Kcp != state.Last && state.File != state.Last {
		switch prefer {
		case PreferKCP:
			return Action{Pull: wantPull, Note: "conflict, kcp wins"}, nil
		case PreferGit:
			return Action{Push: wantPush, Note: "conflict, the file wins"}, nil
		default:
			return Action{}, fmt.Errorf("%w: kcp has %s, the file has %s, the last sync was %s; pass --prefer kcp or --prefer git",
				ErrConflict, short(state.Kcp), short(state.File), short(state.Last))
		}
	}
	action := Action{}
	if wantPush && state.Kcp != state.File && state.Kcp == state.Last {
		action.Push = true
		action.Note = "the file moved"
	}
	if wantPull && state.Kcp != state.File {
		action.Pull = true
		if action.Note == "" {
			action.Note = "kcp moved"
		}
	}
	if action.Note == "" && state.Last == "" {
		action.Note = "no baseline, kcp wins"
		action.Pull = wantPull
	}
	if action.Note == "" && state.Kcp == state.File && state.Last != state.Kcp {
		action.Note = "both sides agree, the baseline was behind"
	}
	if action.Note == "" {
		action.Note = "in step"
	}
	return action, nil
}

func short(hash string) string {
	if hash == "" {
		return "none"
	}
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
