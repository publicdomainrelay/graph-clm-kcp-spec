package e2e

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltgraph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

type backend struct {
	name     string
	options  boltgraph.Options
	password string
}

func backends(t *testing.T) []backend {
	t.Helper()
	out := []backend{
		{
			name: "hydradb",
			options: boltgraph.Options{
				URL:  envOr("SPECD_TEST_HYDRA_URL", "bolt://127.0.0.1:7687"),
				User: envOr("SPECD_TEST_HYDRA_USER", "neo4j"),
			},
			password: envOr("SPECD_TEST_HYDRA_PASSWORD", passwordFromFile(envOr("SPECD_TEST_HYDRA_PASSWORD_FILE", "/tmp/hdb/token"))),
		},
		{
			name: "arcadedb",
			options: boltgraph.Options{
				URL:      envOr("SPECD_TEST_ARCADE_URL", "bolt://127.0.0.1:7688"),
				User:     envOr("SPECD_TEST_ARCADE_USER", "root"),
				Database: envOr("SPECD_TEST_ARCADE_DATABASE", "clm"),
			},
			password: envOr("SPECD_TEST_ARCADE_PASSWORD", "clm-arcadedb-root"),
		},
	}
	for index := range out {
		out[index].options.Password = out[index].password
	}
	return out
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func passwordFromFile(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(contents))
}

func connectBackend(t *testing.T, ctx context.Context, backend backend) *boltgraph.Client {
	t.Helper()
	client, err := boltgraph.Connect(ctx, backend.options, 1)
	if err != nil {
		if os.Getenv("SPECD_REQUIRE_LIVE") == "1" {
			t.Fatalf("%s is not reachable at %s: %v", backend.name, backend.options.URL, err)
		}
		t.Skipf("%s is not reachable at %s: %v", backend.name, backend.options.URL, err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })
	return client
}

// prepareWorkspace clears the names this test owns. The repository goes too so
// ingest creates it with the absolute path of the temporary working tree,
// which is what the graph rebuild resolves code refs against.
func prepareWorkspace(t *testing.T, ctx context.Context, client *kcpclient.Client, names ...string) {
	t.Helper()
	if err := client.Delete(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := client.Delete(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name); err != nil {
			t.Fatal(err)
		}
	}
	contexts := []*spec.SystemContext{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "calc"},
			Spec: spec.SystemContextSpec{
				Repository: "calc",
				Upstream:   spec.RefSelf,
				Intent:     "the calc library, ingested from code",
				Requirements: []spec.Requirement{
					{ID: "r.add-two-ints", Level: spec.LevelMust, Text: "Add returns the sum of two integers.", CodeRefs: []string{"function:Add"}},
				},
				Interfaces: []spec.Interface{
					{Name: "Add", Kind: "function", Signature: "(a, b int) int", File: "calc/calc.go"},
					{Name: "Multiply", Kind: "function", Signature: "(a, b int) int", File: "calc/calc.go"},
				},
				CodeRefs: []string{"function:Add"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc"},
			Spec: spec.SystemContextSpec{
				Repository: "calc",
				Upstream:   "sc.calc",
				Overlay:    []string{"sc.calc"},
				Intent:     "the command line front end",
			},
		},
	}
	for _, context := range contexts {
		context.SetDefaults()
		object, err := kcpclient.Unstructured(context)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Apply(ctx, object); err != nil {
			t.Fatal(err)
		}
	}
}

func liveContextState(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) (int64, string, string) {
	t.Helper()
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, _, err := unstructured.NestedString(object.Object, "status", "observed", "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	return object.GetGeneration(), object.GetResourceVersion(), fingerprint
}

func oneHopKeys(t *testing.T, ctx context.Context, client *boltgraph.Client, label string, id int64, edgeType string, out bool) []string {
	t.Helper()
	keys := []string{}
	for _, edgeSpec := range graph.EdgeSpecs {
		if edgeSpec.Type != edgeType {
			continue
		}
		var (
			rows []map[string]any
			err  error
		)
		switch {
		case out && edgeSpec.FromLabel == label:
			rows, err = client.SelectOut(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, id, graph.LabelProperties[edgeSpec.ToLabel])
		case !out && edgeSpec.ToLabel == label:
			rows, err = client.SelectIn(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, id, graph.LabelProperties[edgeSpec.FromLabel])
		default:
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			keys = append(keys, fmt.Sprint(row[keyPropertyFor(edgeSpec, out)]))
		}
	}
	sort.Strings(keys)
	return keys
}

func keyPropertyFor(edgeSpec graph.EdgeSpec, out bool) string {
	label := edgeSpec.ToLabel
	if !out {
		label = edgeSpec.FromLabel
	}
	switch label {
	case graph.LabelCodeRef:
		return "codegraphId"
	case graph.LabelRequirement:
		return "reqId"
	}
	return "name"
}

func TestPhase2IngestAndGraph(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.Copy(t, "calc")
	prepareWorkspace(t, ctx, client, "calc", "cmd-calc")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.Delete(cleanupCtx, specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
		_ = client.Delete(cleanupCtx, specapi.SystemContextGVR, specapi.DefaultNamespace, "cmd-calc")
		_ = client.Delete(cleanupCtx, specapi.RepositoryGVR, specapi.DefaultNamespace, "calc")
	})

	hydra := connectBackend(t, ctx, backends(t)[0])

	first, err := ingest.Run(ctx, client, ingest.Options{RepoPath: repoPath, Writer: hydra})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if len(first.Contexts) != 2 {
		t.Fatalf("contexts = %+v", first.Contexts)
	}
	library := first.Contexts[0]
	if library.Name != "calc" || len(library.Observed.Files) != 2 || len(library.Observed.Interfaces) != 2 {
		t.Fatalf("observed = %+v", library.Observed)
	}
	if library.Observed.Fingerprint == "" || library.Observed.Interfaces[0].CodegraphID == "" {
		t.Fatalf("observed facts are incomplete: %+v", library.Observed)
	}
	if first.Contexts[1].Name != "cmd-calc" {
		t.Fatalf("second context = %+v", first.Contexts[1])
	}

	generation, resourceVersion, fingerprint := liveContextState(t, ctx, client, "calc")
	if fingerprint != library.Observed.Fingerprint {
		t.Fatalf("status fingerprint = %q, ingest reported %q", fingerprint, library.Observed.Fingerprint)
	}

	second, err := ingest.Run(ctx, client, ingest.Options{RepoPath: repoPath, Writer: hydra})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.Contexts[0].Fingerprint != fingerprint {
		t.Errorf("the fingerprint moved between ingests: %s -> %s", fingerprint, second.Contexts[0].Fingerprint)
	}
	afterGeneration, afterResourceVersion, afterFingerprint := liveContextState(t, ctx, client, "calc")
	if afterGeneration != generation {
		t.Errorf("the second ingest changed the spec generation: %d -> %d", generation, afterGeneration)
	}
	if afterResourceVersion != resourceVersion || afterFingerprint != fingerprint {
		t.Errorf("the second ingest was not a no-op: %s/%s -> %s/%s", resourceVersion, fingerprint, afterResourceVersion, afterFingerprint)
	}
	if second.Contexts[0].SpecChanged || second.Contexts[1].SpecChanged {
		t.Error("the second ingest reported a spec change")
	}

	contextID := graph.ContextID("calc")
	for _, backend := range backends(t) {
		t.Run(backend.name, func(t *testing.T) {
			writer := connectBackend(t, ctx, backend)
			if err := ingest.RebuildGraph(ctx, client, writer, ingest.Options{}); err != nil {
				t.Fatalf("rebuild on %s: %v", backend.name, err)
			}
			declares := oneHopKeys(t, ctx, writer, graph.LabelContext, contextID, graph.EdgeDeclares, true)
			if strings.Join(declares, ",") != "Add,Multiply" {
				t.Errorf("DECLARES = %v, want Add, Multiply", declares)
			}
			requires := oneHopKeys(t, ctx, writer, graph.LabelContext, contextID, graph.EdgeRequires, true)
			if strings.Join(requires, ",") != "r.add-two-ints" {
				t.Errorf("REQUIRES = %v", requires)
			}
			references := oneHopKeys(t, ctx, writer, graph.LabelContext, contextID, graph.EdgeReferences, true)
			want := []string{"file:calc/calc.go", "file:calc/calc_test.go", coderefID(t, library.Observed, "Add")}
			sort.Strings(want)
			if strings.Join(references, ",") != strings.Join(want, ",") {
				t.Errorf("REFERENCES = %v, want %v", references, want)
			}
			upstream := oneHopKeys(t, ctx, writer, graph.LabelContext, contextID, graph.EdgeUpstream, false)
			if strings.Join(upstream, ",") != "cmd-calc" {
				t.Errorf("incoming UPSTREAM = %v, want cmd-calc", upstream)
			}
			contexts, err := writer.Select(ctx, graph.LabelContext, []string{"name"}, map[string]any{"id": contextID})
			if err != nil {
				t.Fatal(err)
			}
			if len(contexts) != 1 || contexts[0]["name"] != "calc" {
				t.Errorf("context vertex = %+v", contexts)
			}
			if ids.Stable("context:calc") != contextID {
				t.Errorf("context id = %d, want the stable id", contextID)
			}
		})
	}
}

func coderefID(t *testing.T, observed spec.ObservedFacts, name string) string {
	t.Helper()
	for _, observedInterface := range observed.Interfaces {
		if observedInterface.Name == name {
			return observedInterface.CodegraphID
		}
	}
	t.Fatalf("%s is not in the observed interfaces: %+v", name, observed.Interfaces)
	return ""
}
