package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const roundTripName = "live-round-trip"

func requireLive(t *testing.T, tools ...string) {
	t.Helper()
	if testing.Short() && os.Getenv("SPECD_REQUIRE_LIVE") != "1" {
		t.Skip("live test skipped in short mode")
	}
	missing := []string{}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) == 0 {
		return
	}
	if os.Getenv("SPECD_REQUIRE_LIVE") == "1" {
		t.Fatalf("SPECD_REQUIRE_LIVE=1 but these tools are missing: %s", strings.Join(missing, ", "))
	}
	t.Skipf("live prerequisites missing: %s", strings.Join(missing, ", "))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func startCluster(t *testing.T, root string) {
	t.Helper()
	command := exec.Command(filepath.Join(root, "deploy", "start-kcp.sh"))
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("deploy/start-kcp.sh failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "kcp ready") {
		t.Fatalf("deploy/start-kcp.sh did not report a ready kcp:\n%s", output)
	}
}

func liveClient(t *testing.T, root string) *kcpclient.Client {
	t.Helper()
	client, err := kcpclient.New(kcpclient.Options{
		Kubeconfig: filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:  "root:specs",
		QPS:        50,
		Burst:      100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func nestedStatusString(t *testing.T, object *unstructured.Unstructured, field string) string {
	t.Helper()
	value, _, err := unstructured.NestedString(object.Object, "status", field)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func nestedSpecString(t *testing.T, object *unstructured.Unstructured, field string) string {
	t.Helper()
	value, _, err := unstructured.NestedString(object.Object, "spec", field)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func conditionOf(t *testing.T, object *unstructured.Unstructured, conditionType string) map[string]any {
	t.Helper()
	conditions, found, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil || !found {
		return nil
	}
	for _, entry := range conditions {
		condition, ok := entry.(map[string]any)
		if ok && condition["type"] == conditionType {
			return condition
		}
	}
	return nil
}

func liveContext() *spec.SystemContext {
	context := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: roundTripName},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Intent:     "Round trip through a real kcp workspace.",
			Requirements: []spec.Requirement{
				{ID: "r.add-two-ints", Level: spec.LevelMust, Text: "Add returns the sum of two integers.", CodeRefs: []string{"function:calc.Add"}},
			},
			Interfaces: []spec.Interface{
				{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go"},
			},
			CodeRefs: []string{"file:calc/calc.go"},
		},
	}
	context.SetDefaults()
	return context
}

func TestPhase1SystemContextRoundTrip(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".kcp-specd", "specs.kubeconfig")); err != nil {
		t.Fatalf("the workspace kubeconfig was not written: %v", err)
	}

	if err := client.Delete(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, roundTripName); err != nil {
		t.Fatal(err)
	}

	typed := liveContext()
	result := spec.ValidateSystemContext(typed)
	if !result.OK() {
		t.Fatalf("the live fixture is invalid: %v", result.Err())
	}
	object, err := kcpclient.Unstructured(typed)
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.Apply(ctx, object)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.GetUID() == "" || created.GetResourceVersion() == "" {
		t.Fatalf("kcp did not stamp identity: %+v", created.Object["metadata"])
	}

	read, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, roundTripName)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	readTyped, err := kcpclient.Typed(read)
	if err != nil {
		t.Fatal(err)
	}
	readContext, ok := readTyped.(*spec.SystemContext)
	if !ok {
		t.Fatalf("read back a %T", readTyped)
	}
	if readContext.Spec.Intent != typed.Spec.Intent || len(readContext.Spec.Requirements) != 1 {
		t.Fatalf("spec did not round trip: %+v", readContext.Spec)
	}
	if readContext.Spec.Upstream != spec.RefSelf {
		t.Fatalf("upstream = %q, want self", readContext.Spec.Upstream)
	}

	patched, err := client.PatchStatus(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, roundTripName, map[string]any{
		"observedCommit":   "deadbeef",
		"realizedSpecHash": result.SpecHash,
		"conditions": []any{map[string]any{
			"type":               specapi.ConditionSpecValid,
			"status":             "True",
			"reason":             specapi.ReasonValidatorPassed,
			"message":            "validator passed",
			"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
		}},
	})
	if err != nil {
		t.Fatalf("patch status: %v", err)
	}
	if got := nestedStatusString(t, patched, "observedCommit"); got != "deadbeef" {
		t.Fatalf("status.observedCommit = %q", got)
	}
	if got := nestedStatusString(t, patched, "realizedSpecHash"); got != result.SpecHash {
		t.Fatalf("status.realizedSpecHash = %q, want %q", got, result.SpecHash)
	}
	condition := conditionOf(t, patched, specapi.ConditionSpecValid)
	if condition == nil || condition["status"] != "True" {
		t.Fatalf("conditions = %+v", patched.Object["status"])
	}

	after, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, roundTripName)
	if err != nil {
		t.Fatal(err)
	}
	if nestedStatusString(t, after, "observedCommit") != "deadbeef" {
		t.Fatal("the status subresource did not persist")
	}
	if nestedSpecString(t, after, "intent") != typed.Spec.Intent {
		t.Fatal("a status write must not touch the spec")
	}
	statusBefore := fmt.Sprintf("%v", after.Object["status"])
	generationBefore := after.GetGeneration()

	// The other half of the subresource contract: a spec write bumps the
	// generation and leaves status exactly as it was.
	editedTyped, err := kcpclient.Typed(after)
	if err != nil {
		t.Fatal(err)
	}
	current, ok := editedTyped.(*spec.SystemContext)
	if !ok {
		t.Fatalf("read back a %T", editedTyped)
	}
	edited := *current
	edited.Spec.Intent = "A human edit, which must not touch status."
	editedObject, err := kcpclient.Unstructured(&edited)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Apply(ctx, editedObject); err != nil {
		t.Fatalf("apply a spec edit: %v", err)
	}
	editedBack, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, roundTripName)
	if err != nil {
		t.Fatal(err)
	}
	if got := nestedSpecString(t, editedBack, "intent"); got != edited.Spec.Intent {
		t.Fatalf("spec.intent = %q, want the edit", got)
	}
	if editedBack.GetGeneration() == generationBefore {
		t.Error("a spec write must bump the generation")
	}
	if got := nestedStatusString(t, editedBack, "observedCommit"); got != "deadbeef" {
		t.Errorf("a spec write changed status.observedCommit to %q", got)
	}
	if got := fmt.Sprintf("%v", editedBack.Object["status"]); got != statusBefore {
		t.Errorf("a spec write changed status:\nbefore %s\nafter  %s", statusBefore, got)
	}

	if err := client.Delete(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, roundTripName); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, roundTripName); !kcpclient.IsNotFound(err) {
		t.Fatalf("read after delete = %v, want not found", err)
	}
}
