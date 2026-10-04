package archkcp

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/archyaml"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const DocumentID = "document"

type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)
	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)
	Delete(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) error
}

type ImportOptions struct {
	Namespace string

	Repository string

	RepositoryPath string

	Prune bool
}

type ImportResult struct {
	Repository string

	Document string

	Names map[string]string

	Contexts int

	Deleted []string
}

func Import(ctx context.Context, cluster Cluster, data []byte, options ImportOptions) (ImportResult, error) {
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	document, err := archyaml.Parse(data)
	if err != nil {
		return ImportResult{}, err
	}
	repository := options.Repository
	if repository == "" {
		repository = documentName(document)
	}
	names := uniqueNames(idsOf(document))

	result := ImportResult{Repository: repository, Names: names}

	repositorySpec := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: repository, Namespace: namespace},
		Spec:       spec.RepositorySpec{Path: repositoryPath(options)},
	}
	repositorySpec.SetDefaults()
	object, err := kcpclient.Unstructured(repositorySpec)
	if err != nil {
		return result, err
	}
	if _, err := cluster.Apply(ctx, object); err != nil {
		return result, err
	}

	wanted := map[string]bool{DocumentID: true}
	for _, node := range document.Nodes() {
		wanted[node.ID] = true
		context := contextFor(node, repository, namespace, names[node.ID])
		encoded, err := kcpclient.Unstructured(context)
		if err != nil {
			return result, err
		}
		if _, err := cluster.Apply(ctx, encoded); err != nil {
			return result, fmt.Errorf("archkcp: apply %s: %w", node.ID, err)
		}
		result.Contexts++
	}

	documentName := documentObjectName(repository)
	result.Document = documentName
	header := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{
			Name:      documentName,
			Namespace: namespace,
			Labels:    archLabels(DocumentID, spec.ArchKindDocument),
		},
		Spec: spec.SystemContextSpec{
			Repository: repository,
			Upstream:   spec.RefSelf,
			Arch:       documentArch(document),
		},
	}
	header.SetDefaults()
	encoded, err := kcpclient.Unstructured(header)
	if err != nil {
		return result, err
	}
	if _, err := cluster.Apply(ctx, encoded); err != nil {
		return result, err
	}

	if options.Prune {
		deleted, err := prune(ctx, cluster, namespace, repository, wanted)
		if err != nil {
			return result, err
		}
		result.Deleted = deleted
	}
	return result, nil
}

type ExportOptions struct {
	Namespace string

	Repository string
}

func Export(ctx context.Context, cluster Cluster, options ExportOptions) ([]byte, error) {
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	listed, err := cluster.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	contexts, err := typedContexts(listed)
	if err != nil {
		return nil, err
	}
	repository, document, err := pickDocument(contexts, options.Repository)
	if err != nil {
		return nil, err
	}
	documentModel := &archyaml.Document{Header: map[string]any{}}
	for key, value := range document.Spec.Arch.Document {
		documentModel.Header[key] = value
	}
	for _, section := range document.Spec.Arch.Sections {
		documentModel.Sections = append(documentModel.Sections, &archyaml.Section{
			Key:  section.Key,
			Form: section.Form,
		})
	}
	index := map[string]*archyaml.Section{}
	for _, section := range documentModel.Sections {
		index[section.Key] = section
	}
	for _, context := range contexts {
		arch := context.Spec.Arch
		if arch == nil || arch.Kind != spec.ArchKindNode || context.Spec.Repository != repository {
			continue
		}
		section, ok := index[arch.Section]
		if !ok {
			section = &archyaml.Section{Key: arch.Section, Form: arch.Form}
			index[arch.Section] = section
			documentModel.Sections = append(documentModel.Sections, section)
		}
		section.Nodes = append(section.Nodes, &archyaml.Node{
			ID:           arch.ID,
			Section:      arch.Section,
			Form:         arch.Form,
			Position:     arch.Position,
			Parent:       arch.Parent,
			Slot:         arch.Slot,
			Body:         arch.Node,
			Upstream:     arch.Upstream,
			Overlay:      arch.Overlay,
			Orchestrator: arch.Orchestrator,
			DependsOn:    arch.DependsOn,
			Introduces:   arch.Introduces,
			Code:         arch.Code,
		})
	}
	return archyaml.Marshal(documentModel)
}

func contextFor(node *archyaml.Node, repository, namespace, name string) *spec.SystemContext {
	context := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    archLabels(node.ID, spec.ArchKindNode),
		},
		Spec: spec.SystemContextSpec{
			Repository:   repository,
			Upstream:     upstreamOf(node),
			Overlay:      archRefs(node.Overlay, spec.RefPrefixContext, spec.RefPrefixOverlay),
			Orchestrator: archRef(node.Orchestrator),
			DependsOn:    archRefs(node.DependsOn, spec.RefPrefixContext),
			Introduces:   archRefs(node.Introduces, spec.RefPrefixContext),
			CodeRefs:     fileRefs(node.Code),
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
				Code:         fileRefs(node.Code),
				Node:         node.Body,
			},
		},
	}
	context.SetDefaults()
	return context
}

func documentArch(document *archyaml.Document) *spec.ArchSpec {
	sections := make([]spec.ArchSection, 0, len(document.Sections))
	for _, section := range document.Sections {
		sections = append(sections, spec.ArchSection{Key: section.Key, Form: section.Form})
	}
	return &spec.ArchSpec{
		ID:       DocumentID,
		Kind:     spec.ArchKindDocument,
		Document: document.Header,
		Sections: sections,
	}
}

func archLabels(id, kind string) map[string]string {
	if id == "" {
		return nil
	}
	return map[string]string{
		specapi.ArchIDLabel:   id,
		specapi.ArchKindLabel: kind,
	}
}

func upstreamOf(node *archyaml.Node) string {
	if ref := archRef(node.Upstream); ref != "" {
		return ref
	}
	return spec.RefSelf
}

func archRef(value string) string {
	if value == "" || value == spec.RefSelf {
		return ""
	}
	if !spec.IsRef(value) {
		return ""
	}
	return value
}

func archRefs(values []string, prefixes ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if archRef(value) == "" || !hasPrefix(value, prefixes) {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func hasPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func fileRefs(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		ref := spec.CodeRefPrefixFile + path
		if spec.IsCodeRef(ref) {
			out = append(out, ref)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func repositoryPath(options ImportOptions) string {
	if options.RepositoryPath != "" {
		return options.RepositoryPath
	}
	return "."
}

func documentName(document *archyaml.Document) string {
	metadata, ok := document.Header["metadata"].(map[string]any)
	if !ok {
		return "arch"
	}
	name, _ := metadata["name"].(string)
	if name == "" {
		return "arch"
	}
	return spec.ArchName(name)
}

func documentObjectName(repository string) string {
	name := spec.ArchName("arch-document-" + repository)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func idsOf(document *archyaml.Document) []string {
	ids := make([]string, 0, len(document.Nodes()))
	for _, node := range document.Nodes() {
		ids = append(ids, node.ID)
	}
	return ids
}

func uniqueNames(ids []string) map[string]string {
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	taken := map[string]bool{}
	names := map[string]string{}
	for _, id := range sorted {
		name := spec.ArchName(id)
		if taken[name] {
			name = name + "-" + shortHash(id)
		}
		for taken[name] {
			name = name + "x"
		}
		taken[name] = true
		names[id] = name
	}
	return names
}

func shortHash(value string) string {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(value))
	digest := hasher.Sum32()
	const hex = "0123456789abcdef"
	out := make([]byte, 8)
	for index := range out {
		out[index] = hex[(digest>>(uint(index)*4))&0xf]
	}
	return string(out)
}

func typedContexts(listed *unstructured.UnstructuredList) ([]*spec.SystemContext, error) {
	out := make([]*spec.SystemContext, 0, len(listed.Items))
	for _, item := range listed.Items {
		typed, err := kcpclient.Typed(&item)
		if err != nil {
			return nil, err
		}
		context, ok := typed.(*spec.SystemContext)
		if !ok {
			continue
		}
		out = append(out, context)
	}
	return out, nil
}

func pickDocument(contexts []*spec.SystemContext, repository string) (string, *spec.SystemContext, error) {
	candidates := []*spec.SystemContext{}
	for _, context := range contexts {
		if context.Spec.Arch == nil || context.Spec.Arch.Kind != spec.ArchKindDocument {
			continue
		}
		if repository != "" && context.Spec.Repository != repository {
			continue
		}
		candidates = append(candidates, context)
	}
	switch len(candidates) {
	case 0:
		return "", nil, fmt.Errorf("archkcp: no imported arch document in this namespace")
	case 1:
		return candidates[0].Spec.Repository, candidates[0], nil
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].Spec.Repository < candidates[right].Spec.Repository
	})
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.Spec.Repository)
	}
	return "", nil, fmt.Errorf("archkcp: more than one arch document (%s), pass --repository", strings.Join(names, ", "))
}

func prune(ctx context.Context, cluster Cluster, namespace, repository string, wanted map[string]bool) ([]string, error) {
	listed, err := cluster.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	deleted := []string{}
	names := []string{}
	byName := map[string]*spec.SystemContext{}
	for _, item := range listed.Items {
		typed, err := kcpclient.Typed(&item)
		if err != nil {
			return nil, err
		}
		context, ok := typed.(*spec.SystemContext)
		if !ok || context.Spec.Arch == nil || context.Spec.Repository != repository {
			continue
		}
		if wanted[context.Spec.Arch.ID] {
			continue
		}
		names = append(names, context.Name)
		byName[context.Name] = context
	}
	sort.Strings(names)
	for _, name := range names {
		if err := cluster.Delete(ctx, specapi.SystemContextGVR, namespace, name); err != nil {
			return deleted, err
		}
		deleted = append(deleted, byName[name].Spec.Arch.ID)
	}
	return deleted, nil
}
