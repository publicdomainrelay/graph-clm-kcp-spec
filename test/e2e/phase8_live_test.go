package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphns"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const phase8Repository = "phase8-calc"

func TestPhase8TheModPathReportsIntoKcp(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", phase8Repository)
	names := []string{"calc", "cmd-calc", phase8Repository, phase8RunningChange}
	repositories := []string{phase8Repository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase8Repository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{"go", "test", "./..."},
		},
	})

	controller, err := specd.New(specd.Options{
		Kubeconfig:   e2eKubeconfig,
		Workspace:    e2eWorkspace,
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
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

	waitFor(t, ctx, "the first ingest of calc", func() bool {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
		if err != nil {
			return false
		}
		realized, _, _ := unstructured.NestedString(object.Object, "status", "realizedSpecHash")
		return realized != ""
	})

	specctl := buildSpecctl(t, root)

	rendered := runSpecctl(t, ctx, specctl, root, nil, "clm", "render", "--context", "calc")
	for _, want := range []string{"# Context: calc", "```yaml spec", "SPECD_MANAGED_BEGIN"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the rendered document has no %q:\n%s", want, rendered)
		}
	}

	edited := addInterface(rendered, spec.Interface{
		Name:      "Subtract",
		Kind:      "function",
		Signature: "func Subtract(a, b int) int",
		File:      "calc/calc.go",
	})
	applied := runSpecctl(t, ctx, specctl, root, []byte(edited), "clm", "apply", "--context", "calc")
	delta := parseDelta(t, applied)
	if len(delta.Interfaces) != 1 || delta.Interfaces[0].Name != "Subtract" {
		t.Fatalf("delta interfaces = %+v, want exactly the added Subtract", delta.Interfaces)
	}
	if len(delta.Requirements) != 0 {
		t.Errorf("delta requirements = %+v, want none", delta.Requirements)
	}

	editedContext := getContext(t, ctx, client, "calc")
	if origin := editedContext.GetAnnotations()[specapi.OriginAnnotation]; origin != specapi.OriginCLM {
		t.Errorf("origin = %q, want %q", origin, specapi.OriginCLM)
	}
	if _, stamped := editedContext.GetAnnotations()[specapi.OriginHashAnnotation]; stamped {
		t.Error("a clm write carries origin-hash, which would hide the edit from the controller")
	}

	var raised spec.SpecChange
	waitFor(t, ctx, "the SpecToCode change for the clm edit", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		if len(found) != 1 {
			return false
		}
		raised = found[0]
		return true
	})
	if raised.Spec.Delta == nil || len(raised.Spec.Delta.Interfaces) != 1 {
		t.Fatalf("the change's delta = %+v, want the one added interface", raised.Spec.Delta)
	}

	running := phase8RunChange(t, ctx, client, "calc")
	queued := runSpecctl(t, ctx, specctl, root, []byte(addInterface(rendered, spec.Interface{
		Name: "Multiply", Kind: "function", Signature: "func Multiply(a, b int) int", File: "calc/calc.go",
	})), "clm", "apply", "--context", "calc")
	if !strings.Contains(queued, "\"interfaces\"") {
		t.Errorf("the queued apply printed no delta:\n%s", queued)
	}
	waitFor(t, ctx, "the second edit's own change", func() bool {
		return len(changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)) == 3
	})

	report := runSpecctl(t, ctx, specctl, root, nil, "clm", "report",
		"--change", running,
		"--event", `{"turn":1,"tool":"Write","files":["calc/calc.go"],"note":"wrote Subtract"}`,
	)
	if !strings.Contains(report, "recorded=true") {
		t.Errorf("report = %q", report)
	}
	foldedChange := getChange(t, ctx, client, running)
	if len(foldedChange.Status.Progress) == 0 {
		t.Fatal("the running change has no progress record")
	}
	last := foldedChange.Status.Progress[len(foldedChange.Status.Progress)-1]
	if len(last.Files) != 1 || last.Files[0] != "calc/calc.go" {
		t.Errorf("progress = %+v", last)
	}
	repeat := runSpecctl(t, ctx, specctl, root, nil, "clm", "report",
		"--change", running,
		"--event", `{"turn":1,"tool":"Write","files":["calc/calc.go"],"note":"wrote Subtract"}`,
	)
	if !strings.Contains(repeat, "recorded=false") {
		t.Errorf("a repeated report was recorded again: %q", repeat)
	}

	phase8AssertTouched(t, root, running)
}

const phase8RunningChange = "phase8-fold-running"

func phase8RunChange(t *testing.T, ctx context.Context, client *kcpclient.Client, systemContext string) string {
	t.Helper()
	applyTyped(t, ctx, client, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: phase8RunningChange, Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: systemContext,
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    strings.Repeat("b", 64),
		},
	})
	if _, err := client.PatchStatus(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace, phase8RunningChange,
		map[string]any{"phase": specapi.PhaseRunning}); err != nil {
		t.Fatalf("put the change into Running: %v", err)
	}
	return phase8RunningChange
}

func phase8AssertTouched(t *testing.T, root, change string) {
	t.Helper()
	fs := flag.NewFlagSet("phase8", flag.ContinueOnError)
	options := boltflags.Add(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := options.Resolve(fs); err != nil {
		t.Fatal(err)
	}
	if !options.Enabled() {
		t.Skip("no graph endpoint is configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	writer, err := options.Connect(ctx)
	if err != nil {
		t.Skipf("no graph at %s: %v", options.URL, err)
	}
	defer writer.Close(ctx)

	rows, err := writer.SelectOut(ctx, graph.EdgeTouched, graph.LabelChange, graph.LabelCodeRef,
		graph.ChangeIDIn(graphns.FromEnv(), change), graph.LabelProperties[graph.LabelCodeRef])
	if err != nil {
		t.Fatalf("read the TOUCHED edges: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("the change %s has no TOUCHED edge", change)
	}
	found := false
	for _, row := range rows {
		if row["codegraphId"] == "file:calc/calc.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("TOUCHED edges = %+v, want file:calc/calc.go on the shared CodeRef id", rows)
	}
	occurred, err := writer.SelectOut(ctx, graph.EdgeOccurred, graph.LabelChange, graph.LabelProgress,
		graph.ChangeIDIn(graphns.FromEnv(), change), graph.LabelProperties[graph.LabelProgress])
	if err != nil {
		t.Fatalf("read the OCCURRED edges: %v", err)
	}
	if len(occurred) == 0 {
		t.Errorf("the change %s has no OCCURRED edge", change)
	}
}

func buildSpecctl(t *testing.T, root string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "specctl")
	command := exec.Command("go", "build", "-o", binary, "./cmd/specctl")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build specctl: %v\n%s", err, output)
	}
	return binary
}

func runSpecctl(t *testing.T, ctx context.Context, binary, root string, stdin []byte, args ...string) string {
	t.Helper()
	full := append(append([]string{}, args...),
		"--kubeconfig", e2eKubeconfig,
		"--workspace", e2eWorkspace,
		"--namespace", specapi.DefaultNamespace,
	)
	command := exec.CommandContext(ctx, binary, full...)
	command.Dir = root
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		t.Fatalf("specctl %s: %v\n%s\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func parseDelta(t *testing.T, stdout string) spec.Delta {
	t.Helper()
	start := strings.Index(stdout, "{")
	if start == -1 {
		t.Fatalf("no delta in %q", stdout)
	}
	out := spec.Delta{}
	if err := json.Unmarshal([]byte(stdout[start:]), &out); err != nil {
		t.Fatalf("read the delta %q: %v", stdout, err)
	}
	return out
}

func addInterface(document string, declared spec.Interface) string {
	block := "```yaml spec"
	open := strings.Index(document, block)
	if open == -1 {
		return document
	}
	body := document[open+len(block):]
	closeAt := strings.Index(body, "```")
	if closeAt == -1 {
		return document
	}
	entry := "  - kind: " + declared.Kind + "\n    name: " + declared.Name + "\n"
	if declared.Signature != "" {
		entry += "    signature: " + declared.Signature + "\n"
	}
	if declared.File != "" {
		entry += "    file: " + declared.File + "\n"
	}
	specBlock := body[:closeAt]
	if !strings.Contains(specBlock, "interfaces:") {
		specBlock += "interfaces:\n"
	}
	specBlock += entry
	return document[:open+len(block)] + specBlock + body[closeAt:]
}

func getChange(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) *spec.SpecChange {
	t.Helper()
	object, err := client.Get(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatalf("read the change %s: %v", name, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change, ok := typed.(*spec.SpecChange)
	if !ok {
		t.Fatalf("%s is not a SpecChange", name)
	}
	return change
}
