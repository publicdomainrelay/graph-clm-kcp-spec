package policy

import (
	"encoding/json"
	"sort"
)

const (
	NodeTextCap = 64 * 1024

	FileTextCap = 256 * 1024
)

type CodeGraphFile struct {
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
	Context  string `json:"context,omitempty"`
	Test     bool   `json:"test"`
	SHA256   string `json:"sha256,omitempty"`
	Size     int    `json:"size,omitempty"`
}

type CodeGraphNode struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualifiedName,omitempty"`
	File          string `json:"file,omitempty"`
	StartLine     int    `json:"startLine,omitempty"`
	EndLine       int    `json:"endLine,omitempty"`
	Exported      bool   `json:"exported,omitempty"`
	Context       string `json:"context,omitempty"`
	Text          string `json:"text,omitempty"`
}

type CodeGraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Line   int    `json:"line,omitempty"`
}

type CodeGraphSpec struct {
	Repository string `json:"repository"`

	Branch string `json:"branch,omitempty"`

	Commit string `json:"commit,omitempty"`

	Files []CodeGraphFile `json:"files"`

	Nodes []CodeGraphNode `json:"nodes"`

	Edges []CodeGraphEdge `json:"edges"`

	Texts map[string]string `json:"texts,omitempty"`

	Effects []Effect `json:"effects,omitempty"`
}

type ObjectMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type CodeGraph struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata ObjectMeta `json:"metadata"`

	Spec CodeGraphSpec `json:"spec"`
}

func (g *CodeGraph) Sort() {
	sort.SliceStable(g.Spec.Files, func(left, right int) bool {
		return g.Spec.Files[left].Path < g.Spec.Files[right].Path
	})
	sort.SliceStable(g.Spec.Nodes, func(left, right int) bool {
		a, b := g.Spec.Nodes[left], g.Spec.Nodes[right]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.StartLine < b.StartLine
	})
	sort.SliceStable(g.Spec.Edges, func(left, right int) bool {
		a, b := g.Spec.Edges[left], g.Spec.Edges[right]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Line < b.Line
	})
}

func (g CodeGraph) Node(id string) (CodeGraphNode, bool) {
	for _, node := range g.Spec.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return CodeGraphNode{}, false
}

func (g CodeGraph) NodesInFile(path string) []CodeGraphNode {
	out := []CodeGraphNode{}
	for _, node := range g.Spec.Nodes {
		if node.File == path {
			out = append(out, node)
		}
	}
	return out
}

type CodeDiffLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

type CodeDiffFile struct {
	Path string `json:"path"`

	Status string `json:"status,omitempty"`

	Added []CodeDiffLine `json:"added,omitempty"`

	Removed []CodeDiffLine `json:"removed,omitempty"`
}

type CodeDiffSpec struct {
	Change string `json:"change,omitempty"`

	// Repository names the repository the diff belongs to, so a rule that
	// reads the diff resolves the CodeGraph of the same evaluation.
	Repository string `json:"repository,omitempty"`

	Base string `json:"base,omitempty"`

	Head string `json:"head,omitempty"`

	Files []CodeDiffFile `json:"files"`
}

type CodeDiff struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata ObjectMeta `json:"metadata"`

	Spec CodeDiffSpec `json:"spec"`
}

func (d *CodeDiff) Sort() {
	sort.SliceStable(d.Spec.Files, func(left, right int) bool {
		return d.Spec.Files[left].Path < d.Spec.Files[right].Path
	})
}

func CodeGraphObject(graph CodeGraph) map[string]any {
	return objectOf(APIVersion, CodeGraphKind, graph.Metadata, graph.Spec)
}

func CodeDiffObject(diff CodeDiff) map[string]any {
	return objectOf(APIVersion, CodeDiffKind, diff.Metadata, diff.Spec)
}

func objectOf(apiVersion, kind string, meta ObjectMeta, spec any) map[string]any {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil
	}
	metadata := map[string]any{"name": meta.Name}
	if meta.Namespace != "" {
		metadata["namespace"] = meta.Namespace
	}
	if len(meta.Labels) > 0 {
		labels := map[string]any{}
		for key, value := range meta.Labels {
			labels[key] = value
		}
		metadata["labels"] = labels
	}
	if len(meta.Annotations) > 0 {
		annotations := map[string]any{}
		for key, value := range meta.Annotations {
			annotations[key] = value
		}
		metadata["annotations"] = annotations
	}
	return map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   metadata,
		"spec":       decoded,
	}
}
