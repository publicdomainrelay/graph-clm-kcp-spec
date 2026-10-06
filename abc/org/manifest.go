package org

import (
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/yamlx"
)

const (
	// MembersPath is the file of the root's architecture branch that lists its
	// members.
	MembersPath = "members.yaml"

	MembersKind = "OrgMembers"
)

type ManifestMetadata struct {
	Name string `json:"name"`

	Branch string `json:"branch,omitempty"`
}

// Manifest is members.yaml: the references from the root's architecture branch
// to the members' branches. It is derived, never edited.
type Manifest struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata ManifestMetadata `json:"metadata"`

	Members []Member `json:"members"`
}

func NewManifest(root, branch string, members []Member) Manifest {
	sorted := append([]Member{}, members...)
	SortMembers(sorted)
	if sorted == nil {
		sorted = []Member{}
	}
	return Manifest{
		APIVersion: specapi.APIVersion,
		Kind:       MembersKind,
		Metadata:   ManifestMetadata{Name: root, Branch: branch},
		Members:    sorted,
	}
}

func (m Manifest) Render() ([]byte, error) {
	return yamlx.Marshal(m)
}

func ParseManifest(data []byte) (Manifest, error) {
	manifest := Manifest{}
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("org: parse %s: %w", MembersPath, err)
	}
	if manifest.Kind != MembersKind {
		return Manifest{}, fmt.Errorf("org: %s is kind %q, not %s", MembersPath, manifest.Kind, MembersKind)
	}
	return manifest, nil
}

func (m Manifest) Member(name string) (Member, bool) {
	for _, member := range m.Members {
		if member.Name == name {
			return member, true
		}
	}
	return Member{}, false
}
