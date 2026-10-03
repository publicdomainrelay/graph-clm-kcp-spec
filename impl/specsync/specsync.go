// Package specsync is the git-native side of the spec mirror: `specctl sync`
// keeps `<repo>/.specs/*.yaml` and kcp in step, so a pull request carries the
// spec and the code together. The format and the conflict rule are pure
// (abc/mirror); this package is the I/O: read the files, read kcp, write the
// one or the other.
package specsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/mirror"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type Cluster interface {
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)

	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

type Options struct {
	Cluster Cluster

	Namespace string

	// Repository is the Repository name whose SystemContexts are mirrored.
	Repository string

	// Dir is the working tree of that repository; the mirror is Dir/.specs.
	Dir string

	Direction mirror.Direction

	Prefer mirror.Prefer
}

type Record struct {
	Context string

	Pull bool

	Push bool

	Note string

	Conflict bool
}

type Result struct {
	Checked int

	Pulled int

	Pushed int

	Skipped int

	Conflicts int

	Records []Record
}

// ErrConflicts is returned when at least one context needs a decision the
// caller did not make. Every other context is still synced, so one conflict
// does not hide the rest.
var ErrConflicts = errors.New("specsync: some contexts conflict")

// Run syncs every SystemContext of one Repository. Direction both pushes
// first and pulls after, so a file a human edited lands in kcp and the file
// then comes back canonical.
func Run(ctx context.Context, options Options) (Result, error) {
	result := Result{}
	if options.Cluster == nil {
		return result, errors.New("specsync: a cluster is required")
	}
	if options.Repository == "" {
		return result, errors.New("specsync: a repository name is required")
	}
	if options.Dir == "" {
		return result, errors.New("specsync: a working tree is required")
	}
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	direction, err := directionOf(options.Direction)
	if err != nil {
		return result, err
	}

	contexts, err := listContexts(ctx, options.Cluster, namespace, options.Repository)
	if err != nil {
		return result, err
	}
	var conflicting []string
	for _, object := range contexts {
		result.Checked++
		record, err := syncOne(ctx, options, namespace, object)
		if err != nil {
			if errors.Is(err, mirror.ErrConflict) {
				result.Conflicts++
				result.Records = append(result.Records, Record{Context: object.GetName(), Conflict: true, Note: err.Error()})
				conflicting = append(conflicting, object.GetName()+": "+err.Error())
				continue
			}
			return result, fmt.Errorf("specsync: %s: %w", object.GetName(), err)
		}
		switch {
		case record.Pull && record.Push:
			result.Pulled++
			result.Pushed++
		case record.Pull:
			result.Pulled++
		case record.Push:
			result.Pushed++
		default:
			result.Skipped++
		}
		if direction == mirror.Both && record.Push {
			// After a push the file has to come back canonical, so the two
			// sides end identical and the annotation names the new baseline.
			pullRecord, err := pullOne(ctx, options, namespace, object.GetName())
			if err != nil {
				return result, fmt.Errorf("specsync: %s: %w", object.GetName(), err)
			}
			if pullRecord.Pull && !record.Pull {
				result.Pulled++
			}
			record.Note = record.Note + "; " + pullRecord.Note
		}
		result.Records = append(result.Records, record)
	}
	if len(conflicting) > 0 {
		return result, fmt.Errorf("%w: %s", ErrConflicts, strings.Join(conflicting, "; "))
	}
	return result, nil
}

func directionOf(direction mirror.Direction) (mirror.Direction, error) {
	if direction == "" {
		return mirror.Both, nil
	}
	return mirror.ParseDirection(string(direction))
}

func syncOne(ctx context.Context, options Options, namespace string, object *unstructured.Unstructured) (Record, error) {
	record := Record{Context: object.GetName()}
	typed, err := typedContext(object)
	if err != nil {
		return record, err
	}
	path := FilePath(options.Dir, object.GetName())
	data, err := os.ReadFile(path)
	hasFile := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return record, err
	}
	state := mirror.State{HasFile: hasFile, Last: typed.GetAnnotations()[specapi.SyncedHashAnnotation]}
	if state.Kcp, err = mirror.DeclaredHash(typed.Spec); err != nil {
		return record, err
	}
	if hasFile {
		parsed, err := mirror.Parse(object.GetName(), data)
		if err != nil {
			return record, err
		}
		if state.File, err = mirror.DeclaredHash(parsed.Spec); err != nil {
			return record, err
		}
	}
	direction, err := directionOf(options.Direction)
	if err != nil {
		return record, err
	}
	action, err := mirror.Resolve(direction, options.Prefer, state)
	if err != nil {
		return record, err
	}
	record.Pull = action.Pull
	record.Push = action.Push
	record.Note = action.Note
	if action.Push {
		if err := push(ctx, options, namespace, object, state); err != nil {
			return record, err
		}
	}
	if action.Pull {
		if err := pull(ctx, options, namespace, typed, state); err != nil {
			return record, err
		}
	}
	return record, nil
}

// pullOne writes one context's file from kcp without consulting the direction
// or the conflict rule. Run uses it to canonicalize a file it just pushed.
func pullOne(ctx context.Context, options Options, namespace, name string) (Record, error) {
	record := Record{Context: name}
	object, err := getContext(ctx, options.Cluster, namespace, name)
	if err != nil {
		return record, err
	}
	typed, err := typedContext(object)
	if err != nil {
		return record, err
	}
	state := mirror.State{HasFile: true, Last: typed.GetAnnotations()[specapi.SyncedHashAnnotation]}
	if state.Kcp, err = mirror.DeclaredHash(typed.Spec); err != nil {
		return record, err
	}
	path := FilePath(options.Dir, name)
	if data, err := os.ReadFile(path); err == nil {
		if parsed, parseErr := mirror.Parse(name, data); parseErr == nil {
			if state.File, err = mirror.DeclaredHash(parsed.Spec); err != nil {
				return record, err
			}
		}
	}
	if state.Kcp == state.File {
		record.Note = "already in step"
		return record, nil
	}
	if err := pull(ctx, options, namespace, typed, state); err != nil {
		return record, err
	}
	record.Pull = true
	record.Note = "canonicalized"
	return record, nil
}

func push(ctx context.Context, options Options, namespace string, object *unstructured.Unstructured, state mirror.State) error {
	typed, err := typedContext(object)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(FilePath(options.Dir, typed.Name))
	if err != nil {
		return err
	}
	parsed, err := mirror.Parse(typed.Name, data)
	if err != nil {
		return err
	}
	merged, err := mirror.Apply(typed.Spec, parsed.Spec)
	if err != nil {
		return err
	}
	next := *typed
	next.Spec = merged
	if result := spec.ValidateSystemContext(&next); !result.OK() {
		return fmt.Errorf("%s does not validate after the merge: %v", typed.Name, result.Err())
	}
	hash, err := mirror.DeclaredHash(merged)
	if err != nil {
		return err
	}
	updated, err := kcpclient.Unstructured(&next)
	if err != nil {
		return err
	}
	updated.SetResourceVersion(object.GetResourceVersion())
	annotations := updated.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	// A pushed spec is a desired state change, so it must not carry the tool's
	// own origin-hash, which would hide it from the controller.
	annotations[specapi.OriginAnnotation] = specapi.OriginGit
	annotations[specapi.SyncedHashAnnotation] = hash
	delete(annotations, specapi.OriginHashAnnotation)
	updated.SetAnnotations(annotations)
	updated.SetNamespace(namespace)
	if _, err := options.Cluster.Apply(ctx, updated); err != nil {
		return fmt.Errorf("write %s to kcp: %w", typed.Name, err)
	}
	return nil
}

func pull(ctx context.Context, options Options, namespace string, typed *spec.SystemContext, state mirror.State) error {
	data, err := mirror.Render(typed.Name, typed.Namespace, typed.Spec)
	if err != nil {
		return err
	}
	path := FilePath(options.Dir, typed.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(data) {
		return annotate(ctx, options, namespace, typed, state.Kcp)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return annotate(ctx, options, namespace, typed, state.Kcp)
}

// annotate moves the synced baseline onto the spec kcp now holds, so the next
// run can tell a file change from a kcp change.
func annotate(ctx context.Context, options Options, namespace string, typed *spec.SystemContext, hash string) error {
	if typed.GetAnnotations()[specapi.SyncedHashAnnotation] == hash {
		return nil
	}
	updated, err := kcpclient.Unstructured(typed)
	if err != nil {
		return err
	}
	annotations := updated.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[specapi.SyncedHashAnnotation] = hash
	updated.SetAnnotations(annotations)
	updated.SetNamespace(namespace)
	if _, err := options.Cluster.Apply(ctx, updated); err != nil {
		return fmt.Errorf("record the synced hash of %s: %w", typed.Name, err)
	}
	return nil
}

// WriteFile renders one context into the mirror of a working tree and reports
// whether the file changed. It touches no cluster, so the spec -> code path can
// write it inside its worktree before the commit, which is what makes one
// commit carry the spec and the code together.
func WriteFile(dir string, context spec.SystemContext) (string, bool, error) {
	data, err := mirror.Render(context.Name, context.Namespace, context.Spec)
	if err != nil {
		return "", false, err
	}
	path := FilePath(dir, context.Name)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(data) {
		return path, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}

// FilePath is where one context's mirror document lives.
func FilePath(dir, name string) string {
	return filepath.Join(dir, mirror.Dir, name+".yaml")
}

func listContexts(ctx context.Context, cluster Cluster, namespace, repository string) ([]*unstructured.Unstructured, error) {
	listed, err := cluster.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, fmt.Errorf("list the system contexts: %w", err)
	}
	out := make([]*unstructured.Unstructured, 0, len(listed.Items))
	for index := range listed.Items {
		item := &listed.Items[index]
		typed, err := typedContext(item)
		if err != nil {
			return nil, err
		}
		if typed.Spec.Repository == repository {
			out = append(out, item)
		}
	}
	return out, nil
}

func getContext(ctx context.Context, cluster Cluster, namespace, name string) (*unstructured.Unstructured, error) {
	listed, err := cluster.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	for index := range listed.Items {
		if listed.Items[index].GetName() == name {
			return &listed.Items[index], nil
		}
	}
	return nil, fmt.Errorf("specsync: there is no SystemContext %s", name)
}

func typedContext(object *unstructured.Unstructured) (*spec.SystemContext, error) {
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	context, ok := typed.(*spec.SystemContext)
	if !ok {
		return nil, fmt.Errorf("specsync: %s is not a SystemContext", object.GetName())
	}
	return context, nil
}
