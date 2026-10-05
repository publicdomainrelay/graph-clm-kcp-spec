package specd

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
)

func TestPolicySyncActionPicksTheSideThatMoved(t *testing.T) {
	recorded := &spec.PolicyStatus{PolicyCommit: "aaa", KcpFingerprint: "k0"}
	cases := []struct {
		name              string
		status            *spec.PolicyStatus
		branchCommit      string
		branchFingerprint string
		kcpFinger         int
		want              syncDirection
	}{
		{name: "no base yet", status: nil, branchCommit: "aaa", branchFingerprint: "k1", want: syncBranchToKcp},
		{name: "fingerprint not recorded yet", status: &spec.PolicyStatus{PolicyCommit: "aaa"},
			branchCommit: "aaa", branchFingerprint: "k1", want: syncBranchToKcp},
		{name: "nothing moved", status: recorded, branchCommit: "aaa", branchFingerprint: "k0", kcpFinger: 0, want: syncNone},
		{name: "the branch moved", status: recorded, branchCommit: "bbb", branchFingerprint: "k1", want: syncBranchToKcp},
		{name: "kcp moved", status: recorded, branchCommit: "aaa", branchFingerprint: "k0", kcpFinger: 1, want: syncKcpToBranch},
		{name: "both moved to the same content", status: recorded, branchCommit: "bbb", branchFingerprint: "k1", kcpFinger: 1, want: syncNone},
		{name: "both moved apart", status: recorded, branchCommit: "bbb", branchFingerprint: "k1", kcpFinger: 2, want: syncConflict},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			kcpFingerprint := recorded.KcpFingerprint
			if item.kcpFinger != 0 {
				kcpFingerprint = "k" + string(rune('0'+item.kcpFinger))
			}
			if item.status == nil {
				kcpFingerprint = ""
			}
			got := policySyncAction(item.status, item.branchCommit, item.branchFingerprint, kcpFingerprint)
			if got != item.want {
				t.Errorf("action = %d, want %d", got, item.want)
			}
		})
	}
}

func persistTemplate(name, slug, kind string) policy.Template {
	return policy.Template{
		Name:  name,
		Slug:  slug,
		Kind:  kind,
		Level: policy.LevelMust,
		Rego:  "package " + name + "\n",
	}
}

func TestPersistPolicyKeepsImportedTemplatesAsImports(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := exec.Command("git", "init", "-q", dir).Run(); err != nil {
		t.Fatal(err)
	}
	store := oagit.Store{Repo: dir}
	ref := policy.RefFor("calc", "main", "main")

	own := persistTemplate("calcown", "calc-own", "CalcOwn")
	packed := persistTemplate("rfppackrule", "rfp-pack-rule", "RfpPackRule")
	branch := policy.Library{
		Manifest: policy.PolicyLibrary{
			Repository: "calc",
			Imports:    []policy.PackImport{{Pack: "rfp-guest-isolation", Version: "1", Source: "embedded"}},
		},
		Imported:    map[string]string{"rfp-pack-rule": "rfp-guest-isolation@1"},
		Templates:   []policy.Template{own, packed},
		Constraints: []policy.Constraint{{Name: "calc-own", Kind: "CalcOwn", Template: "calcown", Enforcement: policy.EnforcementDeny}},
	}
	// The branch as a pre-fix persist left it: the pack template frozen as a
	// local file next to the import and the lock.
	seed := map[string][]byte{
		"templates/calc-own/src.rego":           []byte("package calcown\n"),
		"templates/calc-own/template.yaml":      []byte("kind: ConstraintTemplate\n"),
		"templates/rfp-pack-rule/src.rego":      []byte("package rfppackrule\n"),
		"templates/rfp-pack-rule/template.yaml": []byte("kind: ConstraintTemplate\n"),
		"policies.lock":                         []byte("packs: []\n"),
	}
	branch.Files = seed
	if _, err := policygit.Update(ctx, store, ref, seed, nil, "seed"); err != nil {
		t.Fatal(err)
	}

	// kcp read: the same templates and constraints, but no Imported map.
	kcpLibrary := policy.Library{Templates: branch.Templates, Constraints: branch.Constraints}
	controller := testController(newFakeCluster())
	if err := controller.persistPolicy(ctx, store, ref, &spec.Repository{}, kcpLibrary, branch); err != nil {
		t.Fatal(err)
	}

	tip, err := store.Tip(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	files, err := store.ReadFiles(ctx, tip)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["templates/rfp-pack-rule/src.rego"]; ok {
		t.Errorf("the imported template was frozen into the branch: %v", keys(files))
	}
	if _, ok := files["templates/calc-own/src.rego"]; !ok {
		t.Errorf("the repository's own template is missing: %v", keys(files))
	}
	if got := string(files["policies.lock"]); got != "packs: []\n" {
		t.Errorf("the lock changed: %q", got)
	}
	if _, ok := files["dist/rfp-pack-rule.yaml"]; !ok {
		t.Errorf("the imported template has no dist: %v", keys(files))
	}
	if _, ok := files[filepath.ToSlash(policy.CataloguePath)]; !ok {
		t.Errorf("the catalogue is missing: %v", keys(files))
	}
}

func keys(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}
