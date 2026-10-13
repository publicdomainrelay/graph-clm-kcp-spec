package e2e

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphns"
)

// specctl clm remember writes a memory note into the instance's namespace and
// specctl clm recall reads it back from that namespace alone.
func TestClmRememberThenRecall(t *testing.T) {
	t.Parallel()
	requireLive(t, "bash")
	root := repoRoot(t)
	startCluster(t, root)
	specctl := buildSpecctl(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	title := "the removal guard of " + graphNamespaceFor(t)
	note := "it holds `file:calc/calc.go` and `r.add`"
	runSpecctl(t, ctx, specctl, root, nil, "clm", "remember",
		"--context", "calc", "--title", title, "--body", note, "--turn", "2")

	recalled := runSpecctl(t, ctx, specctl, root, nil, "clm", "recall", "removal", "guard")
	if !strings.Contains(recalled, title) {
		t.Errorf("recall did not find the note it just remembered:\n%s", recalled)
	}

	// The note lives in the instance's namespace: a recall from another
	// namespace finds nothing, which is what the unnamespaced pi id did wrong.
	code, elsewhere, _ := runSpecctlIn(t, ctx, specctl, root, graphNamespaceFor(t)+"-other",
		"clm", "recall", "removal", "guard")
	if code != 0 {
		t.Fatalf("recall in another namespace exited %d", code)
	}
	if strings.Contains(elsewhere, title) {
		t.Errorf("the note leaked into another namespace:\n%s", elsewhere)
	}
}

// runSpecctlIn runs specctl with one graph namespace of its own.
func runSpecctlIn(t *testing.T, ctx context.Context, binary, root, namespace string, args ...string) (int, string, string) {
	t.Helper()
	full := append(append([]string{}, args...),
		"--kubeconfig", e2eKubeconfig,
		"--workspace", clusterWorkspace(t),
		"--namespace", specapi.DefaultNamespace,
	)
	command := exec.CommandContext(ctx, binary, full...)
	command.Dir = root
	command.Env = testEnviron(t, map[string]string{graphns.Env: namespace})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("specctl %s: %v", strings.Join(args, " "), err)
		}
		return command.ProcessState.ExitCode(), stdout.String(), stderr.String()
	}
	return 0, stdout.String(), stderr.String()
}
