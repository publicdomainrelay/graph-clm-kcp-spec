package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const phase7Repository = "phase7-unseen"

// phase7Contexts is what the directory partition of the unseen tree makes: the
// greet module, its format directory, the calc package and its CLI.
var phase7Contexts = []string{"calc-calc", "calc-cmd-calc", "greet", "greet-format"}

// waitForPopulated waits until the manifest has been answered. A Failed phase
// is not a slow populate: it is the answer, so it fails at once with what the
// changes said.
func waitForPopulated(t *testing.T, ctx context.Context, client *kcpclient.Client, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		switch phase7Phase(t, ctx, client) {
		case specapi.PhasePopulated:
			return
		case specapi.PhaseFailed:
			t.Fatalf("the populate failed%s", phase7Status(t, ctx))
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for Populated: %v", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatalf("timed out after %s waiting for Populated%s", timeout, phase7Status(t, ctx))
}

// phase7Status is the detail a timed out populate needs: the phase, the counts
// and what the changes say.
func phase7Status(t *testing.T, ctx context.Context) string {
	t.Helper()
	root := repoRoot(t)
	client, err := kcpclient.New(kcpclient.Options{
		Kubeconfig: filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:  "root:specs",
		QPS:        50,
		Burst:      100,
	})
	if err != nil {
		return ""
	}
	object, err := client.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, phase7Repository)
	if err != nil {
		return ""
	}
	phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
	counts, _, _ := unstructured.NestedMap(object.Object, "status", "contexts")
	changes := ""
	for _, change := range liveSpecChanges(t, ctx, client) {
		changes += "\n  " + change.Name + " " + change.Status.Phase + " " + change.Status.Message
	}
	return "\n  phase " + phase + " contexts " + fmt.Sprint(counts) + changes
}

// phase7Source builds the codebase kcp has never seen: one working tree that
// holds both fixtures, and a bare clone of it to clone from. It returns the
// file url of the bare repository.
func phase7Source(t *testing.T) string {
	t.Helper()
	tree := filepath.Join(t.TempDir(), "unseen")
	fixture.Stage(t, "greet", filepath.Join(tree, "greet"))
	fixture.Stage(t, "calc", filepath.Join(tree, "calc"))
	runGit(t, tree, "init", "-q", "-b", "main")
	runGit(t, tree, "add", "-A")
	runGit(t, tree, "-c", "user.email=fixture@example.com", "-c", "user.name=fixture", "commit", "-qm", "unseen")
	bare := filepath.Join(t.TempDir(), "unseen.git")
	command := exec.Command("git", "clone", "-q", "--bare", tree, bare)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git clone --bare: %v: %s", err, output)
	}
	return "file://" + bare
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

// phase7Scenario copies the example scenario beside the test, because the
// scripted agent reads it from a path.
func phase7Scenario(t *testing.T) string {
	t.Helper()
	source := filepath.Join(repoRoot(t), "examples", "populate", "scenario.yaml")
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

// phase7Run is the whole phase 7 input: one Repository manifest, and a
// controller started with no --agent of its own.
func phase7Run(
	t *testing.T,
	ctx context.Context,
	client *kcpclient.Client,
	root, source string,
	agent *spec.AgentSpec,
	agentTimeout time.Duration,
	maxAttempts int,
) {
	t.Helper()
	forgetObjects(t, ctx, client, []string{phase7Repository}, phase7Contexts)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{phase7Repository}, phase7Contexts)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase7Repository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Source: &spec.RepositorySource{Git: &spec.GitSource{URL: source, Ref: "main"}},
			Populate: &spec.RepositoryPopulate{
				Partition: spec.PartitionDirectory,
				Summarize: true,
				Agent:     agent,
			},
		},
	})

	controller, err := specd.New(specd.Options{
		Kubeconfig:             filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:              "root:specs",
		Namespace:              specapi.DefaultNamespace,
		QPS:                    50,
		Burst:                  100,
		Resync:                 500 * time.Millisecond,
		CacheDir:               filepath.Join(t.TempDir(), "cache"),
		MaxConcurrentSummaries: 2,
		MaxAttempts:            maxAttempts,
		RetryBackoff:           time.Second,
		AgentTimeout:           agentTimeout,
		Log:                    logging.Discard(),
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
}

// TestPhase7OneManifestPopulatesAnUnknownCodebase is the phase 7 acceptance: a
// codebase kcp has never seen, one Repository manifest with a git source and a
// scripted agent, and nothing else. The controller clones, indexes, creates one
// SystemContext per partition and summarizes every one of them, so the phase
// reaches Populated with every context validated.
func TestPhase7OneManifestPopulatesAnUnknownCodebase(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	phase7Run(t, ctx, client, root, phase7Source(t),
		&spec.AgentSpec{Kind: "scripted:" + phase7Scenario(t)}, 3*time.Minute, 2)

	waitForPopulated(t, ctx, client, 3*time.Minute)
	raw := phase7Raw(t, ctx, client)
	repository := phase7RepositoryObject(t, ctx, client)
	if repository.Status.Contexts == nil {
		t.Fatal("status.contexts is empty")
	}
	counts := repository.Status.Contexts
	if counts.Total != len(phase7Contexts) || counts.Summarized != len(phase7Contexts) || counts.Failed != 0 {
		t.Errorf("status.contexts = %+v, want %d total and summarized, none failed", counts, len(phase7Contexts))
	}
	if repository.Status.ResolvedPath == "" || !strings.Contains(repository.Status.ResolvedPath, phase7Repository) {
		t.Errorf("resolvedPath = %q, want the cache checkout", repository.Status.ResolvedPath)
	}
	phase, _, _ := unstructured.NestedString(raw.Object, "status", "phase")
	if phase != specapi.PhasePopulated {
		t.Errorf("phase = %q", phase)
	}
	if condition := conditionOf(t, raw, specapi.ConditionPopulated); condition == nil || condition["status"] != "True" {
		t.Errorf("Populated = %+v, want True", condition)
	}

	// Every context the partition made has a spec, and the validator passed it.
	seen := []string{}
	for _, name := range phase7Contexts {
		object := getContext(t, ctx, client, name)
		if intent := nestedString(t, object, "spec", "intent"); intent == "" {
			t.Errorf("%s has no intent: the manifest asked for a summarize", name)
		}
		if condition := conditionOf(t, object, specapi.ConditionSpecValid); condition == nil || condition["status"] != "True" {
			t.Errorf("%s SpecValid = %+v, want True", name, condition)
		}
		seen = append(seen, name)
	}
	sort.Strings(seen)
	if strings.Join(seen, ",") != strings.Join(phase7Contexts, ",") {
		t.Errorf("contexts = %v, want %v", seen, phase7Contexts)
	}

	// One change per context, all worked off; nothing else was raised.
	ofRepository := []spec.SpecChange{}
	for _, change := range liveSpecChanges(t, ctx, client) {
		for _, name := range phase7Contexts {
			if change.Spec.SystemContext == name {
				ofRepository = append(ofRepository, change)
			}
		}
	}
	if len(ofRepository) != len(phase7Contexts) {
		t.Errorf("changes = %d, want one per context: %+v", len(ofRepository), ofRepository)
	}
	for _, change := range ofRepository {
		if change.Spec.Direction != specapi.DirectionCodeToSpec || change.Status.Phase != specapi.PhaseSucceeded {
			t.Errorf("change %s = %s/%s, want a succeeded CodeToSpec", change.Name, change.Spec.Direction, change.Status.Phase)
		}
	}

	// The summarize left its document in the clone, the only tree there is.
	document := filepath.Join(repository.Status.ResolvedPath, ".specs", "context", "greet.md")
	if _, err := os.Stat(document); err != nil {
		t.Errorf("the context document is missing from the clone: %v", err)
	}

	// Nothing moves once the manifest has been answered.
	quiet := phase4Snapshot(t, ctx, client, phase7Contexts)
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(6 * time.Second):
	}
	if after := phase4Snapshot(t, ctx, client, phase7Contexts); after != quiet {
		t.Errorf("the loop did not settle:\nbefore %s\nafter  %s", quiet, after)
	}
}

// TestPhase7LiveModelPopulatesAnUnknownCodebase is the same manifest with the
// real model: the only test that spends a model call on the populate path.
func TestPhase7LiveModelPopulatesAnUnknownCodebase(t *testing.T) {
	requireLiveModel(t, "deepseek-claude", "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	// A model is not deterministic, so a populate that names it gets one
	// retry: a single bad answer must not fail the acceptance test.
	phase7Run(t, ctx, client, root, phase7Source(t), &spec.AgentSpec{Kind: "claude"}, 10*time.Minute, 2)

	waitForPopulated(t, ctx, client, 20*time.Minute)
	repository := phase7RepositoryObject(t, ctx, client)
	if repository.Status.Contexts == nil || repository.Status.Contexts.Summarized != len(phase7Contexts) {
		t.Fatalf("status.contexts = %+v", repository.Status.Contexts)
	}
	for _, name := range phase7Contexts {
		object := getContext(t, ctx, client, name)
		if intent := nestedString(t, object, "spec", "intent"); intent == "" {
			t.Errorf("%s has no intent", name)
		}
		if condition := conditionOf(t, object, specapi.ConditionSpecValid); condition == nil || condition["status"] != "True" {
			t.Errorf("%s SpecValid = %+v", name, condition)
		}
	}
}

func phase7Raw(t *testing.T, ctx context.Context, client *kcpclient.Client) *unstructured.Unstructured {
	t.Helper()
	object, err := client.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, phase7Repository)
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	return object
}

func phase7RepositoryObject(t *testing.T, ctx context.Context, client *kcpclient.Client) *spec.Repository {
	t.Helper()
	typed, err := kcpclient.Typed(phase7Raw(t, ctx, client))
	if err != nil {
		t.Fatal(err)
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		t.Fatal("not a Repository")
	}
	return repository
}

func phase7Phase(t *testing.T, ctx context.Context, client *kcpclient.Client) string {
	t.Helper()
	object, err := client.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, phase7Repository)
	if err != nil {
		return ""
	}
	phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
	return phase
}

// TestPhase7PackagePartitionAndGlobs covers the two populate inputs the
// directory-only example never exercised: `partition: package`, which groups by
// the directory that holds a package manifest, and include/exclude globs, which
// decide what is indexed at all. The tree is the same unseen one, so the
// difference is entirely the manifest.
func TestPhase7PackagePartitionAndGlobs(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	source := phase7Source(t)
	// Only Go files, and no test files: the greet module is a Deno one, so the
	// package partition of what is left is the calc module alone.
	wantContexts := []string{"calc"}
	forgetObjects(t, ctx, client, []string{phase7Repository}, append(wantContexts, phase7Contexts...))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{phase7Repository}, append(wantContexts, phase7Contexts...))
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase7Repository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Source: &spec.RepositorySource{Git: &spec.GitSource{URL: source, Ref: "main"}},
			Populate: &spec.RepositoryPopulate{
				Partition: spec.PartitionPackage,
				Include:   []string{"**/*.go"},
				Exclude:   []string{"**/*_test.go"},
			},
		},
	})

	controller, err := specd.New(specd.Options{
		Kubeconfig:   filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:    "root:specs",
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
		CacheDir:     filepath.Join(t.TempDir(), "cache"),
		MaxAttempts:  2,
		RetryBackoff: time.Second,
		Log:          logging.Discard(),
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

	waitForPopulated(t, ctx, client, 3*time.Minute)
	repository := phase7RepositoryObject(t, ctx, client)
	if repository.Status.Contexts == nil {
		t.Fatal("status.contexts is empty")
	}
	if got := repository.Status.Contexts.Total; got != len(wantContexts) {
		t.Errorf("contexts.total = %d, want %d: the package partition and the globs decide it", got, len(wantContexts))
	}

	created := []string{}
	for _, context := range liveContexts(t, ctx, client) {
		created = append(created, context.GetName())
	}
	sort.Strings(created)
	if strings.Join(created, ",") != strings.Join(wantContexts, ",") {
		t.Errorf("contexts = %v, want %v: one per package root, filtered by the globs", created, wantContexts)
	}

	// The include/exclude globs decided the files the partition saw.
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, wantContexts[0])
	if err != nil {
		t.Fatalf("read the calc context: %v", err)
	}
	files, _, _ := unstructured.NestedStringSlice(object.Object, "status", "observed", "files")
	sort.Strings(files)
	if strings.Join(files, ",") != "calc/calc/calc.go,calc/cmd/calc/main.go" {
		t.Errorf("observed.files = %v, want the Go files and no test file", files)
	}
}

func liveContexts(t *testing.T, ctx context.Context, client *kcpclient.Client) []unstructured.Unstructured {
	t.Helper()
	listed, err := client.List(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace)
	if err != nil {
		t.Fatalf("list systemcontexts: %v", err)
	}
	return listed.Items
}
