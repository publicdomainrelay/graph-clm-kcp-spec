package realize

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const gateProbeTemplate = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: codediffprobe
spec:
  crd:
    spec:
      names:
        kind: CodeDiffProbe
  targets:
    - target: admission.k8s.gatekeeper.sh
`

const gateProbeSource = `package codediffprobe

import data.lib.specd

violation[{"msg": msg, "details": details}] {
	file := input.review.object.spec.files[_]
	row := file.added[_]
	regex.match("docker", row.text)
	effect := specd.effects[_]
	effect.file == file.path
	effect.line == row.line
	msg := sprintf("added line %s:%d runs an effect the diff does not show", [file.path, row.line])
	details := specd.location(file.path, row.line)
}
`

const gateProbeConstraint = `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: CodeDiffProbe
metadata:
  name: codediffprobe
spec:
  enforcementAction: deny
  match:
    kinds:
    - apiGroups:
      - specs.publicdomainrelay.dev
      kinds:
      - CodeDiff
`

// TestTheGateDiffResolvesTheCodeGraph pins the link the model-form CodeDiff
// rules need at realize time: the diff the gate reviews carries its repository,
// so a rule that reads added lines can also read the effects of the same
// evaluation. Without it the artifacts rules are silent in a realize and only
// run offline.
func TestTheGateDiffResolvesTheCodeGraph(t *testing.T) {
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "gate@example.test")
	gitRun(t, dir, "config", "user.name", "gate")
	source := filepath.Join(dir, "provider.ts")
	if err := os.WriteFile(source, []byte("export function pull() {\n  return 1;\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	base := gitRun(t, dir, "rev-parse", "HEAD")
	changed := "export function pull() {\n  return new Deno.Command(\"docker\", { args: [\"run\", \"--mount\", \"/bin/websocat\", \"guest\"] });\n}\n"
	if err := os.WriteFile(source, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	header, err := policy.ParseTemplate([]byte(gateProbeTemplate), []byte(gateProbeSource))
	if err != nil {
		t.Fatal(err)
	}
	header.Name = "codediffprobe"
	header.Slug = "codediffprobe"
	constraint, err := policy.ParseConstraint([]byte(gateProbeConstraint), "codediffprobe")
	if err != nil {
		t.Fatal(err)
	}
	library := policy.Library{Templates: []policy.Template{header}, Constraints: []policy.Constraint{constraint}}

	decision, report, err := runPolicyGate(context.Background(), Options{
		Repository: &spec.Repository{
			ObjectMeta: metav1.ObjectMeta{Name: "probe", Namespace: "default"},
			Spec:       spec.RepositorySpec{Branch: "main"},
		},
		Base:   base,
		Change: "probe-1",
		Branch: "main",
		Policy: &PolicyGateOptions{Library: library, TestGlobs: []string{"test/**"}},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations) != 1 {
		t.Fatalf("the diff rule did not see the effect of the added line: %+v", report.Violations)
	}
	if !decision.Blocked {
		t.Fatal("the probe did not block")
	}
}
