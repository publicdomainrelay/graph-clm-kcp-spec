package realize

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

// TestTheGateGraphCarriesEffects pins the gate to the same input the offline
// evaluation builds: a policy that reads code_graph.spec.effects must see them
// in a realize exactly as specctl policy eval computes them.
func TestTheGateGraphCarriesEffects(t *testing.T) {
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
	dir, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "market-mini", "compliant"))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := buildGateGraph(context.Background(), Options{
		Repository: &spec.Repository{
			ObjectMeta: metav1.ObjectMeta{Name: "market-mini", Namespace: "default"},
			Spec:       spec.RepositorySpec{Branch: "main"},
		},
		Branch: "main",
		Policy: &PolicyGateOptions{
			TestGlobs: []string{"test/**"},
		},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Spec.Effects) == 0 {
		t.Fatalf("the gate graph carries no effects: %+v", graph.Spec)
	}
	kinds := map[policy.EffectKind]bool{}
	for _, effect := range graph.Spec.Effects {
		kinds[effect.Kind] = true
	}
	if !kinds[policy.EffectProcExec] && !kinds[policy.EffectNetDial] && !kinds[policy.EffectSSHConnect] {
		t.Errorf("the gate graph carries no process or network effect: %v", kinds)
	}
}
