package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/summarize"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

const authorityRepository = "authority-calc"

func TestAuthorityASummaryKeepsWhatAPersonWrote(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", authorityRepository)
	names := []string{"calc", "cmd-calc", authorityRepository}
	forgetObjects(t, ctx, client, []string{authorityRepository}, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{authorityRepository}, names)
	})

	if _, err := ingest.Run(ctx, client, ingest.Options{RepoPath: repoPath, RepositoryName: authorityRepository}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	scenario, err := scriptedagent.LoadBytes([]byte(calcScenario))
	if err != nil {
		t.Fatal(err)
	}
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: authorityRepository, Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: repoPath},
	}
	summarizeCalc := func() summarize.Result {
		t.Helper()
		result, err := summarize.Run(ctx, summarize.Options{
			Cluster: client, Namespace: specapi.DefaultNamespace, Context: "calc",
			Repository: repository, Agent: scriptedagent.New(scenario),
			Codegraph: codegraphcli.Runner{Dir: repoPath},
		})
		if err != nil {
			t.Fatalf("summarize calc: %v", err)
		}
		return result
	}
	if first := summarizeCalc(); len(first.Proposals) != 0 {
		t.Fatalf("the first summary had nothing to keep, yet proposed %+v", first.Proposals)
	}

	edit := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "calc", "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"intent": "A person's paragraph: integer arithmetic with checked division.",
			"requirements": []any{map[string]any{
				"id": "r.divide", "level": "MUST", "text": "Divide by zero returns an error, never panics.",
			}},
		},
	}}
	if _, err := client.ServerSideApply(ctx, edit, "human"); err != nil {
		t.Fatalf("the person's edit: %v", err)
	}

	second := summarizeCalc()
	units := []string{}
	for _, proposal := range second.Proposals {
		units = append(units, proposal.Unit)
	}
	if strings.Join(units, ",") != "intent,requirement/r.divide" {
		t.Errorf("proposals = %+v, want the intent and the removal of r.divide", second.Proposals)
	}

	after := getContext(t, ctx, client, "calc")
	if intent := nestedString(t, after, "spec", "intent"); !strings.HasPrefix(intent, "A person's paragraph") {
		t.Errorf("intent = %q, want the person's paragraph kept", intent)
	}
	requirements := nestedNames(t, after, "spec", "requirements")
	if !containsString(requirements, "r.divide") || !containsString(requirements, "r.add") {
		t.Errorf("requirements = %v, want the person's r.divide beside the summary's", requirements)
	}
	typed, err := kcpclient.Typed(after)
	if err != nil {
		t.Fatal(err)
	}
	current := typed.(*spec.SystemContext)
	hash, err := spec.HashSystemContextSpec(current.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status.RealizedSpecHash == hash {
		t.Error("the summary marked the person's edit realized: no spec to code change would ever implement it")
	}
	if current.Annotations[specapi.OriginHashAnnotation] == hash {
		t.Error("the origin hash names the edited spec, which hides the edit from the spec to code reconcile")
	}

	change := &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-authority", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionCodeToSpec},
	}
	applyTyped(t, ctx, client, change)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.Delete(cleanupCtx, specapi.SpecChangeGVR, specapi.DefaultNamespace, change.Name)
	})
	if _, err := client.PatchStatus(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace, change.Name,
		map[string]any{"proposals": second.Proposals}); err != nil {
		t.Fatalf("patch proposals: %v", err)
	}
	stored, err := client.Get(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace, change.Name)
	if err != nil {
		t.Fatal(err)
	}
	kept, found, err := unstructured.NestedSlice(stored.Object, "status", "proposals")
	if err != nil || !found || len(kept) != len(second.Proposals) {
		t.Errorf("status.proposals = %v (found %v, %v): the CRD must keep them", kept, found, err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
