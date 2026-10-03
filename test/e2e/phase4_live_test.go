package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const phase4Repository = "phase4-calc"

func waitFor(t *testing.T, ctx context.Context, what string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for %s: %v", what, ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func liveSpecChanges(t *testing.T, ctx context.Context, client *kcpclient.Client) []spec.SpecChange {
	t.Helper()
	listed, err := client.List(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	out := []spec.SpecChange{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			t.Fatal(err)
		}
		change, ok := typed.(*spec.SpecChange)
		if ok {
			out = append(out, *change)
		}
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out
}

func changesFor(changes []spec.SpecChange, systemContext, direction string) []spec.SpecChange {
	out := []spec.SpecChange{}
	for _, change := range changes {
		if change.Spec.SystemContext == systemContext && change.Spec.Direction == direction {
			out = append(out, change)
		}
	}
	return out
}

func forgetPhase4(t *testing.T, ctx context.Context, client *kcpclient.Client, repository string, names ...string) {
	t.Helper()
	for _, change := range liveSpecChanges(t, ctx, client) {
		for _, name := range names {
			if change.Spec.SystemContext == name {
				_ = client.Delete(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace, change.Name)
			}
		}
	}
	for _, name := range names {
		_ = client.Delete(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	}
	_ = client.Delete(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, repository)
}

// TestPhase4ControllerDriftAndSpecChanges drives the real controller against
// the real workspace: one commit of code must raise Drifted and one CodeToSpec
// change, one human spec edit must raise one SpecToCode change, and a quiet
// cluster must stay quiet.
func TestPhase4ControllerDriftAndSpecChanges(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", phase4Repository)
	names := []string{"calc", "cmd-calc", phase4Repository}
	forgetPhase4(t, ctx, client, phase4Repository, names...)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetPhase4(t, cleanupCtx, client, phase4Repository, names...)
	})

	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase4Repository, Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: repoPath, Branch: "main", Verify: []string{"go", "test", "./..."}},
	}
	applyTyped(t, ctx, client, repository)

	controller, err := specd.New(specd.Options{
		Kubeconfig: filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:  "root:specs",
		Namespace:  specapi.DefaultNamespace,
		QPS:        50,
		Burst:      100,
		Resync:     500 * time.Millisecond,
		Log:        logging.Discard(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- controller.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		select {
		case err := <-stopped:
			if err != nil {
				t.Errorf("the controller stopped with %v", err)
			}
		case <-time.After(60 * time.Second):
			t.Error("the controller did not stop")
		}
	})

	waitFor(t, ctx, "the first ingest", func() bool {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
		if err != nil {
			return false
		}
		fingerprint, _, _ := unstructured.NestedString(object.Object, "status", "observed", "fingerprint")
		return fingerprint != ""
	})

	first := getContext(t, ctx, client, "calc")
	firstFingerprint := nestedString(t, first, "status", "observed", "fingerprint")
	firstCommit := nestedString(t, first, "status", "observedCommit")
	if synced := nestedString(t, first, "status", "syncedCommit"); synced != firstCommit {
		t.Errorf("syncedCommit = %q, want the first commit %q", synced, firstCommit)
	}
	if synced := nestedString(t, first, "status", "syncedFingerprint"); synced != firstFingerprint {
		t.Errorf("syncedFingerprint = %q, want the first fingerprint %q", synced, firstFingerprint)
	}
	if condition := conditionOf(t, first, specapi.ConditionDrifted); condition == nil || condition["status"] != "False" {
		t.Errorf("the first ingest is not drift: %+v", condition)
	}
	if generation := first.GetGeneration(); nestedInt(t, first, "status", "observedGeneration") != generation {
		t.Errorf("observedGeneration is not the generation %d", generation)
	}
	if changes := liveSpecChanges(t, ctx, client); len(changes) != 0 {
		t.Errorf("a synced cluster raised changes: %+v", changes)
	}

	// The code moves.
	calcFile := filepath.Join(repoPath, "calc", "calc.go")
	contents, err := os.ReadFile(calcFile)
	if err != nil {
		t.Fatal(err)
	}
	appended := string(contents) + "\n// Subtract returns the difference of two integers.\nfunc Subtract(a, b int) int {\n\treturn a - b\n}\n"
	if err := os.WriteFile(calcFile, []byte(appended), 0o644); err != nil {
		t.Fatal(err)
	}
	secondCommit := fixture.Commit(t, repoPath, "add Subtract")

	waitFor(t, ctx, "Drifted=True after the commit", func() bool {
		condition := conditionOf(t, getContext(t, ctx, client, "calc"), specapi.ConditionDrifted)
		return condition != nil && condition["status"] == "True"
	})

	var codeToSpec spec.SpecChange
	waitFor(t, ctx, "the CodeToSpec change", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionCodeToSpec)
		if len(found) != 1 {
			return false
		}
		codeToSpec = found[0]
		return true
	})
	if codeToSpec.Spec.FromCommit != firstCommit || codeToSpec.Spec.ToCommit != secondCommit {
		t.Errorf("CodeToSpec commits = %s -> %s, want %s -> %s",
			codeToSpec.Spec.FromCommit, codeToSpec.Spec.ToCommit, firstCommit, secondCommit)
	}
	if want := spec.ChangeNameCodeToSpec("calc", firstCommit, secondCommit); codeToSpec.Name != want {
		t.Errorf("CodeToSpec name = %q, want %q", codeToSpec.Name, want)
	}
	if codeToSpec.Status.Phase != specapi.PhasePending {
		t.Errorf("CodeToSpec phase = %q, want Pending", codeToSpec.Status.Phase)
	}
	if observed := nestedString(t, getContext(t, ctx, client, "calc"), "status", "observedCommit"); observed != secondCommit {
		t.Errorf("observedCommit = %q, want %q", observed, secondCommit)
	}

	// A human edits the spec.
	edited := getContext(t, ctx, client, "calc")
	typed, err := kcpclient.Typed(edited)
	if err != nil {
		t.Fatal(err)
	}
	current, ok := typed.(*spec.SystemContext)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	human := *current
	human.Spec.Intent = "The calc package, now with subtraction."
	human.Spec.Interfaces = append(append([]spec.Interface{}, human.Spec.Interfaces...),
		spec.Interface{Name: "Subtract", Kind: "function", Signature: "(a, b int) int", File: "calc/calc.go"})
	applyTyped(t, ctx, client, &human)

	var specToCode spec.SpecChange
	waitFor(t, ctx, "the SpecToCode change", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		if len(found) != 1 {
			return false
		}
		specToCode = found[0]
		return true
	})
	if !specapi.IsHash(specToCode.Spec.ToSpecHash) || specToCode.Spec.FromSpecHash == "" {
		t.Errorf("SpecToCode hashes = %q -> %q", specToCode.Spec.FromSpecHash, specToCode.Spec.ToSpecHash)
	}
	if specToCode.Status.Phase != specapi.PhasePending {
		t.Errorf("SpecToCode phase = %q, want Pending", specToCode.Status.Phase)
	}

	// Nothing else may move while the changes wait for an agent.
	before := phase4Snapshot(t, ctx, client, names)
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(4 * time.Second):
	}
	if after := phase4Snapshot(t, ctx, client, names); after != before {
		t.Errorf("the controller looped:\nbefore %s\nafter  %s", before, after)
	}
}

func TestPhase4TypeScriptIngest(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	repoPath := fixture.Copy(t, "greet")
	forgetPhase4(t, ctx, client, "greet", "greet", "format")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		forgetPhase4(t, cleanupCtx, client, "greet", "greet", "format")
	})

	result, err := ingest.Run(ctx, client, ingest.Options{RepoPath: repoPath, RepositoryName: "greet"})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(result.Contexts) != 2 {
		t.Fatalf("contexts = %+v, want the root module and format/", result.Contexts)
	}
	byName := map[string]ingest.ContextResult{}
	for _, context := range result.Contexts {
		byName[context.Name] = context
	}
	module, ok := byName["greet"]
	if !ok {
		t.Fatalf("no greet context in %+v", byName)
	}
	format, ok := byName["format"]
	if !ok {
		t.Fatalf("no format context in %+v", byName)
	}

	if got := observedNames(module.Observed); strings.Join(got, ",") != "Greeter,Greeting,greet,shout" {
		t.Errorf("greet interfaces = %v, want Greeter, Greeting, greet, shout", got)
	}
	if got := observedNames(format.Observed); strings.Join(got, ",") != "titleCase,trimAll" {
		t.Errorf("format interfaces = %v, want titleCase, trimAll", got)
	}
	if !contains(module.Observed.Files, "mod.ts") || !contains(module.Observed.Files, "mod_test.ts") {
		t.Errorf("greet files = %v, want both module files", module.Observed.Files)
	}
	if !contains(format.Observed.Files, "format/mod.ts") {
		t.Errorf("format files = %v", format.Observed.Files)
	}
	for _, observed := range module.Observed.Interfaces {
		if strings.Contains(observed.File, "_test.ts") {
			t.Errorf("a test file contributed the interface %+v", observed)
		}
	}

	second, err := ingest.Run(ctx, client, ingest.Options{RepoPath: repoPath, RepositoryName: "greet"})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.Contexts[0].Fingerprint != result.Contexts[0].Fingerprint {
		t.Errorf("the fingerprint moved: %s -> %s", result.Contexts[0].Fingerprint, second.Contexts[0].Fingerprint)
	}
}

func observedNames(observed spec.ObservedFacts) []string {
	out := []string{}
	for _, observedInterface := range observed.Interfaces {
		out = append(out, observedInterface.Name)
	}
	sort.Strings(out)
	return out
}

func phase4Snapshot(t *testing.T, ctx context.Context, client *kcpclient.Client, names []string) string {
	t.Helper()
	parts := []string{}
	for _, name := range names {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", name, object.GetResourceVersion()))
	}
	for _, change := range liveSpecChanges(t, ctx, client) {
		parts = append(parts, fmt.Sprintf("%s=%s", change.Name, change.ResourceVersion))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func getContext(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) *unstructured.Unstructured {
	t.Helper()
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatalf("get systemcontext %s: %v", name, err)
	}
	return object
}

func nestedString(t *testing.T, object *unstructured.Unstructured, fields ...string) string {
	t.Helper()
	value, _, err := unstructured.NestedString(object.Object, fields...)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func nestedInt(t *testing.T, object *unstructured.Unstructured, fields ...string) int64 {
	t.Helper()
	value, _, err := unstructured.NestedInt64(object.Object, fields...)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func applyTyped(t *testing.T, ctx context.Context, client *kcpclient.Client, object any) {
	t.Helper()
	spec.SetDefaults(object)
	stamped, err := kcpclient.Unstructured(object)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Apply(ctx, stamped); err != nil {
		t.Fatal(err)
	}
}
