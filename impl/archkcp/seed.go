package archkcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/archyaml"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type SeedCluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)

	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

type SeedOptions struct {
	Namespace string

	Repository string

	Data []byte

	Partitions []specsync.Partition

	RootContext bool
}

type SeedResult struct {
	Seeded []string
}

func Seed(ctx context.Context, cluster SeedCluster, options SeedOptions) (SeedResult, error) {
	result := SeedResult{}
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	document, err := archyaml.Parse(options.Data)
	if err != nil {
		return result, err
	}
	matched := specsync.MatchArch(options.Partitions, document)
	if options.RootContext {
		root := specsync.RootArchNodeID(document, options.Repository)
		if root != "" {
			for _, partition := range options.Partitions {
				if partition.Directory == "." {
					matched[partition.Name] = root
				}
			}
		}
	}
	names := make([]string, 0, len(matched))
	contextByNode := map[string]string{}
	for name, id := range matched {
		names = append(names, name)
		contextByNode[id] = name
	}
	sort.Strings(names)
	resolver := &refResolver{document: document, contextByNode: contextByNode}

	for _, name := range names {
		node := document.Node(matched[name])
		if node == nil {
			continue
		}
		seed := seedSpec(node, options.Repository, filesOf(options.Partitions, name), resolver)
		object, err := cluster.Get(ctx, specapi.SystemContextGVR, namespace, name)
		switch {
		case kcpclient.IsNotFound(err):
			continue
		case err != nil:
			return result, err
		}
		typed, err := kcpclient.Typed(object)
		if err != nil {
			return result, err
		}
		current, ok := typed.(*spec.SystemContext)
		if !ok || current.Spec.Repository != options.Repository {
			continue
		}
		merged, changed := mergeSeed(current.Spec, seed)
		if !changed {
			continue
		}
		hash, err := spec.HashSystemContextSpec(merged)
		if err != nil {
			return result, err
		}
		updated := &spec.SystemContext{
			ObjectMeta: *current.ObjectMeta.DeepCopy(),
			Spec:       merged,
			Status:     current.Status,
		}
		if updated.Annotations == nil {
			updated.Annotations = map[string]string{}
		}
		updated.Annotations[specapi.OriginAnnotation] = specapi.OriginIngest
		updated.Annotations[specapi.OriginHashAnnotation] = hash
		updated.SetDefaults()
		encoded, err := kcpclient.Unstructured(updated)
		if err != nil {
			return result, err
		}
		if _, err := cluster.Apply(ctx, encoded); err != nil {
			return result, fmt.Errorf("archkcp: seed %s from %s: %w", name, node.ID, err)
		}
		if _, err := cluster.PatchStatus(ctx, specapi.SystemContextGVR, namespace, name, map[string]any{
			"realizedSpecHash": hash,
			"realizedSpec":     specObject(merged),
		}); err != nil {
			return result, fmt.Errorf("archkcp: seed %s from %s: %w", name, node.ID, err)
		}
		result.Seeded = append(result.Seeded, name)
	}
	return result, nil
}

func filesOf(partitions []specsync.Partition, name string) []string {
	for _, partition := range partitions {
		if partition.Name != name {
			continue
		}
		return append(append([]string{}, partition.Files...), partition.TreeFiles...)
	}
	return nil
}

func knownFiles(paths, files []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, path := range paths {
		for _, file := range files {
			if file != path && !strings.HasPrefix(file, path+"/") {
				continue
			}
			if seen[file] {
				continue
			}
			seen[file] = true
			out = append(out, file)
		}
	}
	sort.Strings(out)
	return out
}

type refResolver struct {
	document *archyaml.Document

	contextByNode map[string]string
}

func (r *refResolver) contextOf(payload string) string {
	if name, ok := r.contextByNode[spec.RefPrefixContext+payload]; ok {
		return name
	}
	want := spec.ArchName(payload)
	for id, name := range r.contextByNode {
		if spec.ArchName(id) == want {
			return name
		}
	}
	return ""
}

func (r *refResolver) nodeFor(payload string) *archyaml.Node {
	if node := r.document.Node(spec.RefPrefixContext + payload); node != nil {
		return node
	}
	want := spec.ArchName(payload)
	for _, node := range r.document.Nodes() {
		if spec.ArchName(node.ID) == want {
			return node
		}
	}
	return nil
}

// resolve follows a ref that names an arch node onto the generated context of
// that node, then onto the upstreams of nodes no generated context carries, so a
// ref never points at a context that does not exist.
func (r *refResolver) resolve(ref string) string {
	if !strings.HasPrefix(ref, spec.RefPrefixContext) {
		return ref
	}
	payload, ok := spec.RefName(ref)
	if !ok {
		return ""
	}
	if name := r.contextOf(payload); name != "" {
		return spec.RefPrefixContext + name
	}
	node := r.nodeFor(payload)
	for hop := 0; node != nil && hop < 8; hop++ {
		next := archRef(node.Upstream)
		if next == "" {
			return ""
		}
		if !strings.HasPrefix(next, spec.RefPrefixContext) {
			return next
		}
		nextPayload, _ := spec.RefName(next)
		if name := r.contextOf(nextPayload); name != "" {
			return spec.RefPrefixContext + name
		}
		node = r.nodeFor(nextPayload)
	}
	return ""
}

func (r *refResolver) direct(ref string) string {
	if !strings.HasPrefix(ref, spec.RefPrefixContext) {
		return ref
	}
	payload, ok := spec.RefName(ref)
	if !ok {
		return ""
	}
	if name := r.contextOf(payload); name != "" {
		return spec.RefPrefixContext + name
	}
	return ""
}

func (r *refResolver) refs(values []string, mode refMode) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		var resolved string
		switch mode {
		case refKeep:
			resolved = r.keep(value)
		case refDirect:
			resolved = r.direct(value)
		default:
			resolved = r.resolve(value)
		}
		if resolved == "" || seen[resolved] {
			continue
		}
		seen[resolved] = true
		out = append(out, resolved)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// keep resolves a ref onto the generated context that carries its node, and
// leaves it spelled as the document does when no generated context carries it:
// an overlay of the document's own contexts stays readable.
func (r *refResolver) keep(ref string) string {
	if !strings.HasPrefix(ref, spec.RefPrefixContext) {
		return ref
	}
	payload, ok := spec.RefName(ref)
	if !ok {
		return ""
	}
	if name := r.contextOf(payload); name != "" {
		return spec.RefPrefixContext + name
	}
	return ref
}

type refMode int

const (
	refKeep refMode = iota

	refDirect

	refFollow
)

func seedSpec(node *archyaml.Node, repository string, files []string, resolver *refResolver) spec.SystemContextSpec {
	code := fileRefs(knownFiles(node.Code, files))
	upstream := resolver.resolve(archRef(node.Upstream))
	if upstream == "" && resolver.document != nil {
		upstream = spec.RefSelf
	}
	return spec.SystemContextSpec{
		Repository:   repository,
		Upstream:     upstream,
		Overlay:      resolver.refs(archRefs(node.Overlay, spec.RefPrefixContext, spec.RefPrefixOverlay), refKeep),
		Orchestrator: archRef(node.Orchestrator),
		DependsOn:    resolver.refs(archRefs(node.DependsOn, spec.RefPrefixContext), refDirect),
		Introduces:   resolver.refs(archRefs(node.Introduces, spec.RefPrefixContext), refDirect),
		CodeRefs:     code,
		Arch: &spec.ArchSpec{
			ID:           node.ID,
			Kind:         spec.ArchKindNode,
			Section:      node.Section,
			Form:         node.Form,
			Position:     node.Position,
			Parent:       node.Parent,
			Slot:         node.Slot,
			Upstream:     archRef(node.Upstream),
			Overlay:      archRefs(node.Overlay, spec.RefPrefixContext, spec.RefPrefixOverlay),
			Orchestrator: archRef(node.Orchestrator),
			DependsOn:    archRefs(node.DependsOn, spec.RefPrefixContext),
			Introduces:   archRefs(node.Introduces, spec.RefPrefixContext),
			Code:         code,
			Node:         nodeBody(node.Body),
		},
	}
}

func mergeSeed(current, seed spec.SystemContextSpec) (spec.SystemContextSpec, bool) {
	merged := current
	if seed.Upstream != "" && (merged.Upstream == "" || merged.Upstream == spec.RefSelf) {
		merged.Upstream = seed.Upstream
	}
	if seed.Orchestrator != "" && merged.Orchestrator == "" {
		merged.Orchestrator = seed.Orchestrator
	}
	merged.Overlay = union(merged.Overlay, seed.Overlay)
	merged.DependsOn = union(merged.DependsOn, seed.DependsOn)
	merged.Introduces = union(merged.Introduces, seed.Introduces)
	merged.CodeRefs = union(merged.CodeRefs, seed.CodeRefs)
	if merged.Arch == nil && seed.Arch != nil {
		arch := *seed.Arch
		merged.Arch = &arch
	}
	if reflect.DeepEqual(spec.Canonicalize(merged), spec.Canonicalize(current)) {
		return current, false
	}
	return merged, true
}

func union(existing, added []string) []string {
	if len(added) == 0 {
		return existing
	}
	return spec.CanonicalSet(append(append([]string{}, existing...), added...))
}

func specObject(value spec.SystemContextSpec) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func nodeBody(body map[string]any) map[string]any {
	if len(body) == 0 {
		return nil
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return body
	}
	out := map[string]any{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return body
	}
	return out
}
