package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/summarize"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const phase5Repository = "phase5-calc"

// calcScenario answers every context of the calc fixture, so whichever one
// drifts the controller can work it off.
const calcScenario = `
contexts:
  calc:
    summary: The calc package does arithmetic on two integers.
    intent: Arithmetic on two integers, as a library the CLI wraps.
    requirements:
      - id: r.add
        level: MUST
        text: Add returns the sum of two integers.
        codeRefs: ["Add", "file:calc/calc.go"]
      - id: r.multiply
        level: MUST
        text: Multiply returns the product of two integers.
        codeRefs: ["Multiply", "file:calc/calc.go"]
      - id: r.subtract
        level: SHOULD
        text: Subtract returns the difference of two integers.
        codeRefs: ["Subtract", "file:calc/calc.go"]
    interfaces:
      - name: Add
        kind: function
        signature: "func Add(a, b int) int"
        file: calc/calc.go
      - name: Multiply
        kind: function
        signature: "func Multiply(a, b int) int"
        file: calc/calc.go
      - name: Subtract
        kind: function
        signature: "func Subtract(a, b int) int"
        file: calc/calc.go
  cmd-calc:
    summary: The calc command line front end.
    intent: A command line front end over the calc package.
    requirements:
      - id: r.read-operands
        level: MUST
        text: The CLI reads two integers and an operator.
        codeRefs: ["file:cmd/calc/main.go"]
    interfaces:
      - name: main
        kind: function
        file: cmd/calc/main.go
  phase5-calc:
    summary: The module root of the calc fixture.
    intent: The Go module that holds the calc library and its CLI.
    requirements:
      - id: r.module
        level: MUST
        text: The module is example.com/calc.
        codeRefs: ["file:go.mod"]
    interfaces: []
`

// greetScenario answers the two contexts of the Deno/TypeScript fixture.
const greetScenario = `
contexts:
  greet:
    summary: The greet module turns a name into a greeting.
    intent: A small library that greets a person, with a formatting helper.
    requirements:
      - id: r.greet
        level: MUST
        text: greet returns a greeting for a name.
        codeRefs: ["file:mod.ts"]
      - id: r.shout
        level: SHOULD
        text: shout greets in upper case.
        codeRefs: ["file:mod.ts"]
    interfaces: []
  format:
    summary: Text helpers used by the greet module.
    intent: Formatting helpers for names and titles.
    requirements:
      - id: r.title-case
        level: SHOULD
        text: titleCase title cases a string.
        codeRefs: ["file:format/mod.ts"]
    interfaces: []
`

func phase5Scenario(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(path, []byte(calcScenario), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPhase5CodeToSpecWithTheScriptedAgent drives the whole loop against the
// live workspace: a commit drifts one context, the controller raises a
// CodeToSpec change, the agent answers, the spec is written with the ingest
// origin, and the episode ends without raising the opposite direction.
func TestPhase5CodeToSpecWithTheScriptedAgent(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", phase5Repository)
	names := []string{"calc", "cmd-calc", phase5Repository}
	repositories := []string{phase5Repository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase5Repository, Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: repoPath, Branch: "main", Verify: []string{"go", "test", "./..."}},
	})

	controller, err := specd.New(specd.Options{
		Kubeconfig:   filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:    "root:specs",
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
		Agent:        "scripted:" + phase5Scenario(t),
		MaxAttempts:  3,
		RetryBackoff: time.Second,
		Log:          logging.Discard(),
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
	if changes := liveSpecChanges(t, ctx, client); len(changes) != 0 {
		t.Errorf("a freshly ingested cluster raised changes: %+v", changes)
	}

	// The code moves: Subtract joins the calc package.
	calcFile := filepath.Join(repoPath, "calc", "calc.go")
	contents, err := os.ReadFile(calcFile)
	if err != nil {
		t.Fatal(err)
	}
	appended := string(contents) + "\n// Subtract returns the difference of two integers.\nfunc Subtract(a, b int) int {\n\treturn a - b\n}\n"
	if err := os.WriteFile(calcFile, []byte(appended), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.Commit(t, repoPath, "add Subtract")

	waitFor(t, ctx, "the CodeToSpec change to succeed", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionCodeToSpec)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhaseSucceeded
	})

	updated := getContext(t, ctx, client, "calc")
	if intent := nestedString(t, updated, "spec", "intent"); !strings.Contains(intent, "Arithmetic on two integers") {
		t.Errorf("intent = %q, want the agent's paragraph", intent)
	}
	if annotation := nestedString(t, updated, "metadata", "annotations", specapi.OriginAnnotation); annotation != specapi.OriginIngest {
		t.Errorf("origin annotation = %q, want %q", annotation, specapi.OriginIngest)
	}
	interfaces := nestedNames(t, updated, "spec", "interfaces")
	if strings.Join(interfaces, ",") != "Add,Multiply,Subtract" {
		t.Errorf("interfaces = %v, want the three the agent declared", interfaces)
	}
	requirements := nestedNames(t, updated, "spec", "requirements")
	if len(requirements) != 3 {
		t.Errorf("requirements = %v", requirements)
	}

	// The realized hash is the hash of the spec that was written, so the write
	// is not a pending human edit.
	typed, err := kcpclient.Typed(updated)
	if err != nil {
		t.Fatal(err)
	}
	current := typed.(*spec.SystemContext)
	hash, err := spec.HashSystemContextSpec(current.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status.RealizedSpecHash != hash {
		t.Errorf("realizedSpecHash = %q, want %q", current.Status.RealizedSpecHash, hash)
	}
	if current.Status.SyncedFingerprint != current.Status.Observed.Fingerprint {
		t.Errorf("syncedFingerprint = %q, want the observed fingerprint", current.Status.SyncedFingerprint)
	}
	if condition := conditionOf(t, updated, specapi.ConditionDrifted); condition == nil || condition["status"] != "False" {
		t.Errorf("Drifted = %+v, want False", condition)
	}
	if found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode); len(found) != 0 {
		t.Errorf("the ingest write raised a SpecToCode change: %+v", found)
	}

	// The context document is a real file in the managed tree.
	document, err := os.ReadFile(filepath.Join(repoPath, ".specs", "context", "calc.md"))
	if err != nil {
		t.Fatalf("the context document was not written: %v", err)
	}
	if !strings.Contains(string(document), "calc package does arithmetic") {
		t.Errorf("document = %q, want the summary", document)
	}
	if !strings.Contains(string(document), "function:") {
		t.Errorf("document = %q, want the resolved code refs", document)
	}

	// Nothing else may move once the episode is over.
	before := phase5Snapshot(t, ctx, client, names)
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(4 * time.Second):
	}
	if after := phase5Snapshot(t, ctx, client, names); after != before {
		t.Errorf("the loop did not settle:\nbefore %s\nafter  %s", before, after)
	}
}

// TestPhase5IngestSummarizeFillsEmptySpecs runs the same summarize the CLI runs:
// ingest first, then one summarize per context whose intent is still empty.
func TestPhase5IngestSummarizeFillsEmptySpecs(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.Copy(t, "greet")
	forgetObjects(t, ctx, client, []string{"greet"}, []string{"greet", "format"})
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{"greet"}, []string{"greet", "format"})
	})

	result, err := ingest.Run(ctx, client, ingest.Options{RepoPath: repoPath, RepositoryName: "greet"})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(result.Contexts) == 0 {
		t.Fatal("the ingest produced no context")
	}

	scenario, err := scriptedagent.LoadBytes([]byte(greetScenario))
	if err != nil {
		t.Fatal(err)
	}
	scripted := scriptedagent.New(scenario)
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "greet", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: repoPath},
	}

	summarized := 0
	for _, contextResult := range result.Contexts {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, contextResult.Name)
		if err != nil {
			t.Fatal(err)
		}
		typed, err := kcpclient.Typed(object)
		if err != nil {
			t.Fatal(err)
		}
		if typed.(*spec.SystemContext).Spec.Intent != "" {
			continue
		}
		summary, err := summarize.Run(ctx, summarize.Options{
			Cluster: client, Namespace: specapi.DefaultNamespace, Context: contextResult.Name,
			Repository: repository, Agent: scripted,
			Codegraph: codegraphcli.Runner{Dir: repoPath},
		})
		if err != nil {
			t.Fatalf("summarize %s: %v", contextResult.Name, err)
		}
		if !summary.Applied {
			t.Errorf("%s was summarized but its spec did not change", contextResult.Name)
		}
		summarized++
	}
	if summarized != len(result.Contexts) {
		t.Errorf("summarized %d of %d contexts", summarized, len(result.Contexts))
	}

	// A second pass finds every intent filled and touches nothing.
	second, err := ingest.Run(ctx, client, ingest.Options{RepoPath: repoPath, RepositoryName: "greet"})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	for _, contextResult := range second.Contexts {
		if contextResult.StatusWrote || contextResult.SpecChanged {
			t.Errorf("the second ingest wrote %s: %+v", contextResult.Name, contextResult)
		}
	}
}

func nestedNames(t *testing.T, object *unstructured.Unstructured, fields ...string) []string {
	t.Helper()
	entries, found, err := unstructured.NestedSlice(object.Object, fields...)
	if err != nil || !found {
		t.Fatalf("no %v on %s: %v", fields, object.GetName(), err)
	}
	out := []string{}
	for _, entry := range entries {
		mapping, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"name", "id"} {
			if value, ok := mapping[key].(string); ok && value != "" {
				out = append(out, value)
				break
			}
		}
	}
	return out
}

func phase5Snapshot(t *testing.T, ctx context.Context, client *kcpclient.Client, names []string) string {
	t.Helper()
	return phase4Snapshot(t, ctx, client, names)
}
