package agent

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

// The context document is a real file in the managed repository. The model owns
// everything above the markers; the part between them is regenerated from the
// resolved code refs on every summarize, so a model edit there is lost. This is
// the pi-hydradb-clm pattern, with specd's own marker names.
const (
	ManagedBegin = "<!-- SPECD_MANAGED_BEGIN -->"

	ManagedEnd = "<!-- SPECD_MANAGED_END -->"

	ContextDir = ".specs/context"

	DefaultManagedBudget = 1500
)

// ResolvedRef is a code reference the index answered: the stable CodeGraph id
// and the facts that describe it. The managed zone lists these, so the model
// can cite an id instead of re-deriving one.
type ResolvedRef struct {
	CodegraphID string

	Kind string

	Name string

	FilePath string
}

// FileRef is the resolved reference of one `file:` code ref. A file is already
// keyed by its CodeGraph id, so it needs no lookup to belong in the managed
// zone; a symbol ref does, which is why only files are built this way.
func FileRef(codeRef string) (ResolvedRef, bool) {
	filePath, ok := strings.CutPrefix(codeRef, "file:")
	if !ok || filePath == "" {
		return ResolvedRef{}, false
	}
	return ResolvedRef{
		CodegraphID: codeRef,
		Kind:        "file",
		Name:        path.Base(filePath),
		FilePath:    filePath,
	}, true
}

func ContextDocPath(repoPath, context string) string {
	return path.Join(repoPath, ContextDir, context+".md")
}

// SplitContextDoc separates the model zone from the managed zone. A document
// with no markers is all model zone, which is what the first summarize sees.
func SplitContextDoc(text string) (string, string) {
	begin := strings.Index(text, ManagedBegin)
	end := strings.Index(text, ManagedEnd)
	if begin == -1 || end == -1 || end < begin {
		return strings.TrimSpace(text), ""
	}
	model := strings.TrimSpace(text[:begin] + text[end+len(ManagedEnd):])
	return model, strings.TrimSpace(text[begin : end+len(ManagedEnd)])
}

// ComposeContextDoc puts the model zone above a freshly rendered managed zone.
// An empty model zone becomes a placeholder, so the file always tells the model
// where to write.
func ComposeContextDoc(model string, refs []ResolvedRef, budget int) string {
	if budget <= 0 {
		budget = DefaultManagedBudget
	}
	head := strings.TrimSpace(model)
	if head == "" {
		head = "# Context\n\n_(empty: the model writes its summary here)_"
	}
	return head + "\n\n" + RenderManagedZone(refs, budget) + "\n"
}

func RenderManagedZone(refs []ResolvedRef, budget int) string {
	builder := strings.Builder{}
	builder.WriteString(ManagedBegin + "\n")
	builder.WriteString("## Resolved code references\n")
	if len(refs) == 0 {
		builder.WriteString("\n_None yet._\n")
		builder.WriteString(ManagedEnd)
		return builder.String()
	}

	builder.WriteString("\n")
	order := append([]ResolvedRef{}, refs...)
	sortRefs(order)
	spent := 0
	shown := 0
	for _, ref := range order {
		line := fmt.Sprintf("- `%s` %s %s (%s)\n", ref.CodegraphID, ref.Kind, ref.Name, ref.FilePath)
		cost := EstimateTokens(line)
		if spent+cost > budget {
			break
		}
		spent += cost
		shown++
		builder.WriteString(line)
	}
	if hidden := len(order) - shown; hidden > 0 {
		fmt.Fprintf(&builder, "\n_%d more reference(s) indexed but not listed here to stay inside the %d-token budget._\n", hidden, budget)
	}
	builder.WriteString(ManagedEnd)
	return builder.String()
}

func sortRefs(refs []ResolvedRef) {
	sort.Slice(refs, func(left, right int) bool { return refs[left].CodegraphID < refs[right].CodegraphID })
}

func ObservedRefs(observed spec.ObservedFacts) []ResolvedRef {
	out := make([]ResolvedRef, 0, len(observed.Interfaces))
	for _, observedInterface := range observed.Interfaces {
		if observedInterface.CodegraphID == "" {
			continue
		}
		out = append(out, ResolvedRef{
			CodegraphID: observedInterface.CodegraphID,
			Kind:        observedInterface.Kind,
			Name:        observedInterface.Name,
			FilePath:    observedInterface.File,
		})
	}
	return out
}

func ContextRefs(contextSpec spec.SystemContextSpec, observed spec.ObservedFacts) []ResolvedRef {
	refs := ObservedRefs(observed)
	seen := map[string]bool{}
	for _, ref := range refs {
		seen[ref.CodegraphID] = true
	}
	for _, codeRef := range contextSpec.CodeRefs {
		fileRef, ok := FileRef(codeRef)
		if !ok || seen[fileRef.CodegraphID] {
			continue
		}
		seen[fileRef.CodegraphID] = true
		refs = append(refs, fileRef)
	}
	return refs
}
