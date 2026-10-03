package e2e

import (
	"context"
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
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const (
	phase9TenantA = "phase9-a"

	phase9TenantB = "phase9-b"

	phase9Provider = "root:specs-provider"

	phase9Export = "specs.publicdomainrelay.dev"

	phase9RepositoryA = "phase9-a-repo"

	phase9RepositoryB = "phase9-b-repo"
)

// TestPhase9TwoTenantsOneExportController is the multi workspace end to end:
// two tenant workspaces bind the same APIExport, each holds its own Repository
// and its own codebase, and one specd in export mode reconciles both through
// the APIExport virtual workspace. Drift in one tenant must not touch the
// other, and each tenant's status must be written back to its own logical
// cluster.
func TestPhase9TwoTenantsOneExportController(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	admin := liveClient(t, root)
	if err := admin.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	for _, workspace := range []string{phase9TenantA, phase9TenantB} {
		bindWorkspace(t, ctx, root, workspace)
	}
	tenantA := tenantClient(t, root, phase9TenantA)
	tenantB := tenantClient(t, root, phase9TenantB)

	repoA := fixture.CopyAs(t, "calc", phase9RepositoryA)
	repoB := fixture.CopyAs(t, "greet", phase9RepositoryB)
	forgetObjects(t, ctx, tenantA, []string{phase9RepositoryA}, []string{"calc", "cmd-calc", phase9RepositoryA})
	forgetObjects(t, ctx, tenantB, []string{phase9RepositoryB}, []string{phase9RepositoryB, "format"})
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, tenantA, []string{phase9RepositoryA}, []string{"calc", "cmd-calc", phase9RepositoryA})
		forgetObjects(t, cleanupCtx, tenantB, []string{phase9RepositoryB}, []string{phase9RepositoryB, "format"})
		deleteWorkspace(t, cleanupCtx, root, phase9TenantA)
		deleteWorkspace(t, cleanupCtx, root, phase9TenantB)
	})

	applyTyped(t, ctx, tenantA, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase9RepositoryA, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoA,
			Branch: "main",
			Verify: []string{"go", "test", "./..."},
		},
	})
	applyTyped(t, ctx, tenantB, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase9RepositoryB, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoB,
			Branch: "main",
			Verify: []string{"deno", "test"},
		},
	})

	controller, err := specd.New(specd.Options{
		Kubeconfig:        filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:         "root:specs",
		Namespace:         specapi.DefaultNamespace,
		Mode:              specd.ModeExport,
		ProviderWorkspace: phase9Provider,
		ExportName:        phase9Export,
		QPS:               50,
		Burst:             100,
		Resync:            500 * time.Millisecond,
		MaxAttempts:       3,
		RetryBackoff:      time.Second,
		Log:               logging.Discard(),
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

	// Both tenants are reconciled by the one controller, each in its own
	// logical cluster.
	waitFor(t, ctx, "the ingest of both tenants", func() bool {
		return ingested(t, ctx, tenantA, "calc") && ingested(t, ctx, tenantB, phase9RepositoryB)
	})
	fingerprintA := fingerprintOf(t, ctx, tenantA, "calc")
	fingerprintB := fingerprintOf(t, ctx, tenantB, phase9RepositoryB)
	if fingerprintA == "" || fingerprintB == "" {
		t.Fatalf("fingerprints = %q and %q", fingerprintA, fingerprintB)
	}

	// The code of tenant A moves. Tenant B must not notice: no drift, the same
	// fingerprint, and no change.
	calcFile := filepath.Join(repoA, "calc", "calc.go")
	contents, err := os.ReadFile(calcFile)
	if err != nil {
		t.Fatal(err)
	}
	appended := string(contents) + "\n// Subtract returns the difference of two integers.\nfunc Subtract(a, b int) int {\n\treturn a - b\n}\n"
	if err := os.WriteFile(calcFile, []byte(appended), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.Commit(t, repoA, "add Subtract")

	waitFor(t, ctx, "Drifted=True in tenant A", func() bool {
		condition := conditionOf(t, getContext(t, ctx, tenantA, "calc"), specapi.ConditionDrifted)
		return condition != nil && condition["status"] == "True"
	})
	// The change is raised by the next reconcile of the context, which is a
	// separate cycle from the ingest that set the condition.
	waitFor(t, ctx, "the CodeToSpec change in tenant A", func() bool {
		return len(changesFor(liveSpecChanges(t, ctx, tenantA), "calc", specapi.DirectionCodeToSpec)) == 1
	})
	// Tenant B is read after tenant A has fully reacted, so this is the claim
	// that drift in one tenant does not touch the other, not a timing artifact.
	if got := fingerprintOf(t, ctx, tenantB, phase9RepositoryB); got != fingerprintB {
		t.Errorf("tenant B's fingerprint moved from %s to %s", fingerprintB, got)
	}
	condition := conditionOf(t, getContext(t, ctx, tenantB, phase9RepositoryB), specapi.ConditionDrifted)
	if condition != nil && condition["status"] == "True" {
		t.Error("tenant B drifted when only tenant A's code moved")
	}
	// The change tenant A's drift raised is in tenant A's cluster, and tenant B
	// holds none: the controller wrote each object back to the logical cluster
	// the watch said it came from.
	changesA := changesFor(liveSpecChanges(t, ctx, tenantA), "calc", specapi.DirectionCodeToSpec)
	if len(changesA) != 1 {
		t.Errorf("tenant A has %d CodeToSpec changes, want 1", len(changesA))
	}
	if changes := liveSpecChanges(t, ctx, tenantB); len(changes) != 0 {
		t.Errorf("tenant B raised changes: %+v", changes)
	}
}

// bindWorkspace runs deploy/bind-workspace.sh, which is the documented way to
// create a tenant and bind the export; the test drives it rather than a second
// implementation of it.
func bindWorkspace(t *testing.T, ctx context.Context, root, workspace string) {
	t.Helper()
	command := exec.CommandContext(ctx, filepath.Join(root, "deploy", "bind-workspace.sh"), workspace)
	command.Dir = root
	command.Env = append(os.Environ(), "WAIT_SECONDS=180")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("deploy/bind-workspace.sh %s: %v\n%s", workspace, err, output)
	}
	if !strings.Contains(string(output), "workspace:  root:"+workspace) {
		t.Fatalf("bind-workspace.sh did not bind %s:\n%s", workspace, output)
	}
}

func deleteWorkspace(t *testing.T, ctx context.Context, root, workspace string) {
	t.Helper()
	command := exec.CommandContext(ctx, "kubectl", "--kubeconfig",
		filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		"delete", "workspace", workspace, "--wait=false")
	command.Dir = root
	_ = command.Run()
}

func tenantClient(t *testing.T, root, workspace string) *kcpclient.Client {
	t.Helper()
	client, err := kcpclient.New(kcpclient.Options{
		Kubeconfig: filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:  "root:" + workspace,
		QPS:        50,
		Burst:      100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func ingested(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) bool {
	t.Helper()
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		return false
	}
	observed, _, _ := unstructured.NestedFieldNoCopy(object.Object, "status", "observed", "files")
	files, _ := observed.([]any)
	return len(files) > 0
}

func fingerprintOf(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) string {
	t.Helper()
	value, _, _ := unstructured.NestedFieldNoCopy(getContext(t, ctx, client, name).Object, "status", "observed", "fingerprint")
	text, _ := value.(string)
	return text
}
