package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/archyaml"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/archkcp"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const archRepository = "deno-kcp"

// TestPhase3ArchRoundTrip imports the open architecture document into kcp,
// exports it again, and compares the two models. It owns the names under
// repository deno-kcp and deletes them when it finishes.
func TestPhase3ArchRoundTrip(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	clearArchRepository(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		clearArchRepository(t, cleanupCtx, client)
	})

	data, err := os.ReadFile(filepath.Join(root, "testdata", "open-architecture", "arch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := archyaml.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	result, err := archkcp.Import(ctx, client, data, archkcp.ImportOptions{
		Repository:     archRepository,
		RepositoryPath: root,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Contexts != len(original.Nodes()) {
		t.Fatalf("imported %d contexts, want %d", result.Contexts, len(original.Nodes()))
	}

	exported, err := archkcp.Export(ctx, client, archkcp.ExportOptions{Repository: archRepository})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	reparsed, err := archyaml.Parse(exported)
	if err != nil {
		t.Fatalf("the exported document does not parse: %v", err)
	}
	if differences := archyaml.Diff(original, reparsed); len(differences) > 0 {
		t.Fatalf("the round trip through kcp changed the document:\n%s", strings.Join(differences, "\n"))
	}

	// A second import must be a no-op and the export must not move.
	if _, err := archkcp.Import(ctx, client, data, archkcp.ImportOptions{Repository: archRepository, RepositoryPath: root}); err != nil {
		t.Fatalf("second import: %v", err)
	}
	again, err := archkcp.Export(ctx, client, archkcp.ExportOptions{Repository: archRepository})
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(exported) {
		t.Fatal("the second import changed the exported document")
	}

	// The imported contexts are ordinary contexts, so the phase 2 graph code
	// indexes them and the neighborhood is reachable by arch id.
	backend := backends(t)[0]
	writer := connectBackend(t, ctx, backend)
	if err := ingest.RebuildGraph(ctx, client, writer, ingest.Options{}); err != nil {
		t.Fatalf("rebuild on %s: %v", backend.name, err)
	}
	contextID := graph.ContextID(spec.ArchName("sc.deno-kcp"))
	out := oneHopKeys(t, ctx, writer, graph.LabelContext, contextID, graph.EdgeUpstream, true)
	if !contains(out, spec.ArchName("sc.kcp-local")) {
		t.Errorf("UPSTREAM out of sc.deno-kcp = %v", out)
	}
	in := oneHopKeys(t, ctx, writer, graph.LabelContext, contextID, graph.EdgeUpstream, false)
	if !contains(in, spec.ArchName("sc.kind.denopod")) {
		t.Errorf("UPSTREAM into sc.deno-kcp = %v", in)
	}
	dependencies := oneHopKeys(t, ctx, writer, graph.LabelContext, contextID, graph.EdgeOverlay, true)
	if len(dependencies) == 0 {
		t.Errorf("OVERLAY out of sc.deno-kcp is empty")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func clearArchRepository(t *testing.T, ctx context.Context, client *kcpclient.Client) {
	t.Helper()
	listed, err := client.List(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := []string{}
	for _, item := range listed.Items {
		repository, _, _ := unstructured.NestedString(item.Object, "spec", "repository")
		if repository != archRepository {
			continue
		}
		names = append(names, item.GetName())
	}
	for _, name := range names {
		if err := client.Delete(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name); err != nil {
			t.Fatalf("delete %s: %v", name, err)
		}
	}
	if err := client.Delete(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, archRepository); err != nil {
		t.Fatalf("delete the repository: %v", err)
	}
}
