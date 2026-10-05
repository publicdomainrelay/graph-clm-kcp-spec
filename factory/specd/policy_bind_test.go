package specd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

// The bind prompt must carry the code and no binding: the repository's own
// model, with the roles, the vocabulary and the component globs stripped, and
// an empty Binding, so the answer cannot ride in the request the harness reads
// (plan 0010 D1; review 0006 N10). The fixture is perturbed -- the requester
// directory is renamed -- so a prompt built from a copy of the hand-written
// binding would still name the old path and fail here.
func TestTheBindRequestCarriesTheCodeAndNoBinding(t *testing.T) {
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
	dir := fixture.CopyTree(t, filepath.Join("market-mini", "compliant"))
	perturbRequester(t, dir)

	graph, err := codegraphfacts.Build(context.Background(), dir, codegraphfacts.Options{Repository: "market-mini"})
	if err != nil {
		t.Fatal(err)
	}
	computed, err := effects.Apply(&graph, effects.Options{IncludeExtras: true})
	if err != nil {
		t.Fatal(err)
	}
	source := repositoryModelSource{repository: "market-mini", graph: graph, effects: computed}

	binding := policy.Binding{
		Roles: map[string]policy.RoleBinding{"guest": {Globs: []string{"lib/cloud-init/**"}}},
		Vocabulary: policy.Vocabulary{
			Channels: map[string][]string{"relay": {"websocat", "fedproxy"}},
		},
	}
	change := &policy.PolicyChange{Spec: policy.PolicyChangeSpec{Repository: "market-mini", Pack: "rfp-guest-isolation"}}
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "market-mini", Namespace: "default"},
		Spec:       spec.RepositorySpec{Branch: "main"},
	}
	request, err := (&Controller{}).policyGenerateRequest(change, repository, dir, source, binding, nil, nil, policy.Library{}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Binding.RoleNames()) != 0 || len(request.Binding.Vocabulary.Channels) != 0 {
		t.Errorf("the request carries the binding: %+v", request.Binding)
	}
	for _, leaked := range []string{"websocat", "fedproxy", "lib/cloud-init/**", "- guest"} {
		if strings.Contains(request.Model, leaked) {
			t.Errorf("the bind model carries %q:\n%s", leaked, request.Model)
		}
	}
	if !strings.Contains(request.Model, "lib/caller/mod.ts") {
		t.Errorf("the bind model is not derived from the perturbed repository:\n%s", request.Model)
	}
}

func perturbRequester(t *testing.T, dir string) {
	t.Helper()
	if err := os.Rename(filepath.Join(dir, "lib", "requester"), filepath.Join(dir, "lib", "caller")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "deno.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.ReplaceAll(string(data), "./lib/requester", "./lib/caller")
	if updated == string(data) {
		t.Fatal("the manifest does not name the requester directory")
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}
