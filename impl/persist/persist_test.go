package persist

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type fakeCluster struct {
	objects map[string]*unstructured.Unstructured
	writes  int
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: map[string]*unstructured.Unstructured{}}
}

func key(resource, name string) string {
	return resource + "/" + name
}

func (f *fakeCluster) Get(_ context.Context, gvr schema.GroupVersionResource, _ string, name string) (*unstructured.Unstructured, error) {
	object, ok := f.objects[key(gvr.Resource, name)]
	if !ok {
		return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
	}
	return object.DeepCopy(), nil
}

func (f *fakeCluster) List(_ context.Context, gvr schema.GroupVersionResource, _ string) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	for name, object := range f.objects {
		if strings.HasPrefix(name, gvr.Resource+"/") {
			list.Items = append(list.Items, *object.DeepCopy())
		}
	}
	return list, nil
}

func (f *fakeCluster) Apply(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	resource := specapi.ResourceForKind(object.GetKind())
	stored := object.DeepCopy()
	if existing, ok := f.objects[key(resource, object.GetName())]; ok {
		if status, found, _ := unstructured.NestedMap(existing.Object, "status"); found {
			_ = unstructured.SetNestedMap(stored.Object, status, "status")
		}
		stored.SetGeneration(existing.GetGeneration() + 1)
	} else {
		stored.SetGeneration(1)
	}
	f.writes++
	stored.SetResourceVersion(string(rune('a' + f.writes)))
	f.objects[key(resource, object.GetName())] = stored
	return stored.DeepCopy(), nil
}

func (f *fakeCluster) PatchStatus(_ context.Context, gvr schema.GroupVersionResource, _ string, name string, status map[string]any) (*unstructured.Unstructured, error) {
	object, ok := f.objects[key(gvr.Resource, name)]
	if !ok {
		return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
	}
	current, _, _ := unstructured.NestedMap(object.Object, "status")
	if current == nil {
		current = map[string]any{}
	}
	for field, value := range status {
		current[field] = value
	}
	if err := unstructured.SetNestedMap(object.Object, current, "status"); err != nil {
		return nil, err
	}
	return object.DeepCopy(), nil
}

func (f *fakeCluster) put(t *testing.T, value any) {
	t.Helper()
	object, err := kcpclient.Unstructured(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Apply(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	if status, found, _ := unstructured.NestedMap(object.Object, "status"); found {
		resource := specapi.ResourceForKind(object.GetKind())
		_ = unstructured.SetNestedMap(f.objects[key(resource, object.GetName())].Object, status, "status")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func clone(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "calc.go"), []byte("package calc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "upstream code")
	return dir
}

func seed(t *testing.T, cluster *fakeCluster, repoPath string) {
	t.Helper()
	repository := spec.Repository{}
	repository.APIVersion = specapi.Group + "/" + specapi.Version
	repository.Kind = specapi.RepositoryKind
	repository.Name = "calc"
	repository.Namespace = "default"
	repository.Spec.Source = &spec.RepositorySource{Path: repoPath}
	repository.Status.ResolvedPath = repoPath
	repository.Status.Phase = "Populated"
	cluster.put(t, &repository)

	context := spec.SystemContext{}
	context.APIVersion = specapi.Group + "/" + specapi.Version
	context.Kind = specapi.SystemContextKind
	context.Name = "calc"
	context.Namespace = "default"
	context.Annotations = map[string]string{specapi.OriginAnnotation: specapi.OriginIngest}
	context.Spec = spec.SystemContextSpec{
		Repository:   "calc",
		Upstream:     "self",
		Intent:       "Integer arithmetic.",
		Requirements: []spec.Requirement{{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum."}},
		Interfaces:   []spec.Interface{{Name: "Add", Kind: "function"}},
	}
	cluster.put(t, &context)
}

func readContext(t *testing.T, cluster *fakeCluster, name string) spec.SystemContext {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, "default", name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	return *typed.(*spec.SystemContext)
}

func TestPersistOnAFreshCloneMakesOneOrphanCommitAndNoTreeChange(t *testing.T) {
	repo := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, repo)
	ctx := context.Background()

	result, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed || result.Parent != "" {
		t.Fatalf("result = %+v", result)
	}
	if out, err := exec.Command("git", "-C", repo, "merge-base", "main", oabranch.Branch("calc")).CombinedOutput(); err == nil {
		t.Fatalf("not an orphan: %s", out)
	}
	if got := git(t, repo, "status", "--porcelain"); got != "" {
		t.Fatalf("the project tree changed: %q", got)
	}
	if got := git(t, repo, "ls-files"); got != "calc.go" {
		t.Fatalf("the project index changed: %q", got)
	}
	if !strings.Contains(git(t, repo, "show", oabranch.Branch("calc")+":specs/calc.yaml"), "Integer arithmetic.") {
		t.Fatal("the spec is not on the branch")
	}
	repository, _ := cluster.Get(ctx, specapi.RepositoryGVR, "default", "calc")
	if commit, _, _ := unstructured.NestedString(repository.Object, "status", "openArchitecture", "commit"); commit != result.Commit {
		t.Fatalf("status records %q, want %q", commit, result.Commit)
	}

	again, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Committed || again.Commit != result.Commit {
		t.Fatalf("an unchanged kcp made a commit: %+v", again)
	}

	edited := readContext(t, cluster, "calc")
	edited.Spec.Intent = "Integer arithmetic, edited."
	edited.Annotations[specapi.OriginAnnotation] = specapi.OriginCLM
	cluster.put(t, &edited)
	third, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if !third.Committed || third.Parent != result.Commit {
		t.Fatalf("third = %+v", third)
	}
	message := git(t, repo, "log", "-1", "--format=%B", oabranch.Branch("calc"))
	if !strings.Contains(message, "M specs/calc.yaml") || !strings.Contains(message, "origin=clm") {
		t.Fatalf("message:\n%s", message)
	}
	if subject := strings.SplitN(message, "\n", 2)[0]; subject != "spec(calc): ~intent" {
		t.Fatalf("subject = %q", subject)
	}
}

func seedChange(t *testing.T, cluster *fakeCluster, name string, phase string) {
	t.Helper()
	change := spec.SpecChange{}
	change.APIVersion = specapi.Group + "/" + specapi.Version
	change.Kind = specapi.SpecChangeKind
	change.Name = name
	change.Namespace = "default"
	change.Spec = spec.SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionSpecToCode, ToSpecHash: strings.Repeat("a", 64)}
	change.Status = spec.SpecChangeStatus{Phase: phase, Commit: "c0ffee"}
	cluster.put(t, &change)
}

func TestSpecCommitCarriesItsSpecChangeTrailer(t *testing.T) {
	repo := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, repo)
	seedChange(t, cluster, "calc-s2c-aaaaaaaaaaaa", specapi.PhaseSucceeded)
	if _, err := Persist(context.Background(), Options{Cluster: cluster, Repository: "calc"}); err != nil {
		t.Fatal(err)
	}
	message := git(t, repo, "log", "-1", "--format=%B", oabranch.Branch("calc"))
	if !strings.Contains(message, "Spec-Change: calc-s2c-aaaaaaaaaaaa") {
		t.Fatalf("a spec commit carries no Spec-Change trailer:\n%s", message)
	}
}

func TestProgressOnlyUpdatesDoNotCommit(t *testing.T) {
	repo := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, repo)
	seedChange(t, cluster, "calc-s2c-aaaaaaaaaaaa", specapi.PhaseSucceeded)
	ctx := context.Background()
	first, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	object, err := cluster.Get(ctx, specapi.SpecChangeGVR, "default", "calc-s2c-aaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change := typed.(*spec.SpecChange)
	change.Status.Progress = append(change.Status.Progress, spec.ProgressRecord{Turn: 1, Tool: "edit", Note: "a turn", At: "2026-01-01T00:00:00Z"})
	cluster.put(t, change)

	second, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Committed {
		t.Fatalf("a progress-only update made a commit: %+v", second)
	}
	if tip := git(t, repo, "rev-parse", oabranch.Branch("calc")); tip != first.Commit {
		t.Fatal("the branch moved")
	}
	if len(second.Deferred) != 1 {
		t.Fatalf("deferred = %v", second.Deferred)
	}
}

func TestAFeatureBranchCarriesAChangesDocument(t *testing.T) {
	repo := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, repo)
	ctx := context.Background()
	if _, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"}); err != nil {
		t.Fatal(err)
	}
	edited := readContext(t, cluster, "calc")
	edited.Spec.Requirements = append(edited.Spec.Requirements,
		spec.Requirement{ID: "r.sub", Level: spec.LevelMust, Text: "Subtract returns the difference."})
	cluster.put(t, &edited)
	if _, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc", CodeBranch: "spec/bob"}); err != nil {
		t.Fatal(err)
	}
	document := git(t, repo, "show", "open-architecture/calc--spec-bob:CHANGES.md")
	for _, want := range []string{
		"# Changes on `open-architecture/calc--spec-bob`",
		"added `r.sub` (MUST)",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("CHANGES.md lacks %q:\n%s", want, document)
		}
	}
	if arch := git(t, repo, "show", "open-architecture/calc--spec-bob:arch.yaml"); !strings.Contains(arch, "branch: open-architecture/calc--spec-bob") || !strings.Contains(arch, "kind: GeneratedArchitecture") {
		t.Errorf("arch.yaml does not name the branch it was written to:\n%s", arch)
	}
	if main := git(t, repo, "show", "open-architecture/calc:arch.yaml"); strings.Contains(main, "spec-bob") {
		t.Errorf("the feature's branch name leaked into main's arch.yaml:\n%s", main)
	}
}

func TestAFeatureBranchKeepsTheDefaultBranchesChangeRecords(t *testing.T) {
	repo := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, repo)
	seedChange(t, cluster, "calc-s2c-aaaaaaaaaaaa", specapi.PhaseSucceeded)
	ctx := context.Background()
	if _, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"}); err != nil {
		t.Fatal(err)
	}
	withoutChanges := newFakeCluster()
	seed(t, withoutChanges, repo)
	if _, err := Persist(ctx, Options{Cluster: withoutChanges, Repository: "calc", CodeBranch: "spec/bob"}); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "show", "open-architecture/calc--spec-bob:changes/calc-s2c-aaaaaaaaaaaa.yaml").CombinedOutput(); err != nil {
		t.Fatalf("the feature branch dropped an inherited change record: %v\n%s", err, out)
	}
}

func TestAFreshKcpKeepsTheAttemptRecordsTheBranchHolds(t *testing.T) {
	repo := clone(t)
	ctx := context.Background()

	older := newFakeCluster()
	seed(t, older, repo)
	seedChange(t, older, "calc-s2c-aaaaaaaaaaaa-a2", specapi.PhaseFailed)
	if _, err := Persist(ctx, Options{Cluster: older, Repository: "calc"}); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "show", oabranch.Branch("calc")+":changes/calc-s2c-aaaaaaaaaaaa-a2.yaml").CombinedOutput(); err != nil {
		t.Fatalf("the first persist did not record the attempt: %v\n%s", err, out)
	}

	fresh := newFakeCluster()
	seed(t, fresh, repo)
	seedChange(t, fresh, "calc-s2c-aaaaaaaaaaaa", specapi.PhaseSucceeded)
	if _, err := Persist(ctx, Options{Cluster: fresh, Repository: "calc"}); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "show", oabranch.Branch("calc")+":changes/calc-s2c-aaaaaaaaaaaa.yaml").CombinedOutput(); err != nil {
		t.Fatalf("the re-render did not record the surviving change: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "show", oabranch.Branch("calc")+":changes/calc-s2c-aaaaaaaaaaaa-a2.yaml").CombinedOutput(); err != nil {
		t.Fatalf("the re-render from a fresh kcp deleted the attempt the branch held: %v\n%s", err, out)
	}
}

func commitOnBranch(t *testing.T, repo, path, content string) {
	t.Helper()
	worktree := filepath.Join(t.TempDir(), "edit")
	git(t, repo, "worktree", "add", "-q", worktree, oabranch.Branch("calc"))
	if err := os.WriteFile(filepath.Join(worktree, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, worktree, "add", path)
	git(t, worktree, "-c", "user.name=h", "-c", "user.email=h@h", "commit", "-q", "-m", "a reviewed spec edit")
	git(t, repo, "worktree", "remove", "--force", worktree)
}

func TestABranchEditFlowsIntoKcpAndStaysInHistory(t *testing.T) {
	repo := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, repo)
	ctx := context.Background()
	first, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	file := git(t, repo, "show", oabranch.Branch("calc")+":specs/calc.yaml")
	commitOnBranch(t, repo, "specs/calc.yaml", strings.Replace(file, "Integer arithmetic.", "Integer arithmetic, from a pull request.", 1)+"\n")
	human := git(t, repo, "rev-parse", oabranch.Branch("calc"))

	result, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Imported, ",") != "calc" {
		t.Fatalf("result = %+v", result)
	}
	context := readContext(t, cluster, "calc")
	if context.Spec.Intent != "Integer arithmetic, from a pull request." || context.Annotations[specapi.OriginAnnotation] != specapi.OriginGit {
		t.Fatalf("kcp = %q origin %q", context.Spec.Intent, context.Annotations[specapi.OriginAnnotation])
	}
	if result.Commit != human {
		if got := git(t, repo, "rev-parse", result.Commit+"^"); got != human {
			t.Fatalf("the human commit is not in history: parent %s want %s", got, human)
		}
	}
	if result.Commit == first.Commit {
		t.Fatal("the branch did not move")
	}
}

func TestABranchEditThatCollidesWithKcpIsReportedNotWritten(t *testing.T) {
	repo := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, repo)
	ctx := context.Background()
	if _, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"}); err != nil {
		t.Fatal(err)
	}
	file := git(t, repo, "show", oabranch.Branch("calc")+":specs/calc.yaml")
	commitOnBranch(t, repo, "specs/calc.yaml", strings.Replace(file, "Integer arithmetic.", "The branch says this.", 1)+"\n")
	edited := readContext(t, cluster, "calc")
	edited.Spec.Intent = "kcp says that."
	cluster.put(t, &edited)

	result, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Conflicts["calc"], ",") != "intent" {
		t.Fatalf("conflicts = %+v", result.Conflicts)
	}
	if got := readContext(t, cluster, "calc").Spec.Intent; got != "kcp says that." {
		t.Fatalf("kcp was overwritten: %q", got)
	}
	repository, _ := cluster.Get(ctx, specapi.RepositoryGVR, "default", "calc")
	conflicts, _, _ := unstructured.NestedStringSlice(repository.Object, "status", "openArchitecture", "conflicts")
	if strings.Join(conflicts, ",") != "calc: intent" {
		t.Fatalf("status conflicts = %v", conflicts)
	}
	if message := git(t, repo, "log", "-1", "--format=%B", oabranch.Branch("calc")); !strings.Contains(message, "Conflict: calc intent kept from kcp over branch commit") {
		t.Fatalf("the conflict is not in the branch history:\n%s", message)
	}
}

func TestRestoreRebuildsKcpFromABranchOnTheRemote(t *testing.T) {
	origin := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, origin)
	ctx := context.Background()
	if _, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"}); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", "-q", origin, fresh).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	empty := newFakeCluster()
	result, err := Restore(ctx, RestoreOptions{Cluster: empty, Repository: "calc", RepoPath: fresh, Remote: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Fetched || strings.Join(result.Contexts, ",") != "calc" {
		t.Fatalf("result = %+v", result)
	}
	if got := readContext(t, empty, "calc").Spec.Intent; got != "Integer arithmetic." {
		t.Fatalf("restored intent %q", got)
	}
	repository, err := empty.Get(ctx, specapi.RepositoryGVR, "default", "calc")
	if err != nil {
		t.Fatal(err)
	}
	if path, _, _ := unstructured.NestedString(repository.Object, "spec", "source", "path"); path != fresh {
		t.Fatalf("source path %q, want the local clone %q", path, fresh)
	}
	if got := git(t, fresh, "status", "--porcelain"); got != "" {
		t.Fatalf("the clone's tree changed: %q", got)
	}
}

func TestRestoreWithoutABranchSaysSo(t *testing.T) {
	repo := clone(t)
	_, err := Restore(context.Background(), RestoreOptions{Cluster: newFakeCluster(), Repository: "calc", RepoPath: repo, Remote: "origin"})
	if err == nil {
		t.Fatal("restore of a repository with no branch succeeded")
	}
}

func TestPersistRefusesASubdirectoryOfAnotherRepository(t *testing.T) {
	repo := clone(t)
	sub := filepath.Join(repo, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cluster := newFakeCluster()
	seed(t, cluster, sub)
	_, err := Persist(context.Background(), Options{Cluster: cluster, Repository: "calc"})
	if !errors.Is(err, ErrNotTopLevel) {
		t.Fatalf("err = %v, want ErrNotTopLevel", err)
	}
	if out, _ := exec.Command("git", "-C", repo, "branch", "--list", "open-architecture/*").CombinedOutput(); strings.TrimSpace(string(out)) != "" {
		t.Fatalf("a branch was written into the enclosing repository: %s", out)
	}
}

func TestAFeatureBranchPersistsToItsOwnArchitectureBranch(t *testing.T) {
	repo := clone(t)
	cluster := newFakeCluster()
	seed(t, cluster, repo)
	ctx := context.Background()
	mainResult, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if mainResult.Branch != "open-architecture/calc" {
		t.Fatalf("main persisted to %s", mainResult.Branch)
	}

	edited := readContext(t, cluster, "calc")
	edited.Spec.Intent = "Integer arithmetic, with bob."
	cluster.put(t, &edited)
	feature, err := Persist(ctx, Options{Cluster: cluster, Repository: "calc", CodeBranch: "spec/bob"})
	if err != nil {
		t.Fatal(err)
	}
	if feature.Branch != "open-architecture/calc--spec-bob" || !feature.Committed {
		t.Fatalf("feature = %+v", feature)
	}
	if parent := git(t, repo, "rev-parse", feature.Commit+"^"); parent != mainResult.Commit {
		t.Fatalf("the feature architecture does not branch off main's: parent %s, want %s", parent, mainResult.Commit)
	}
	if tip := git(t, repo, "rev-parse", "open-architecture/calc"); tip != mainResult.Commit {
		t.Fatal("main's architecture moved with the feature branch")
	}
	if strings.Contains(git(t, repo, "show", "open-architecture/calc:specs/calc.yaml"), "with bob") {
		t.Fatal("the feature's spec leaked into main's architecture")
	}

	fresh := filepath.Join(t.TempDir(), "fresh")
	if out, err := exec.Command("git", "clone", "-q", repo, fresh).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	git(t, fresh, "fetch", "-q", "origin", "+refs/heads/open-architecture/*:refs/remotes/origin/open-architecture/*")
	onMain, err := Restore(ctx, RestoreOptions{Cluster: newFakeCluster(), Repository: "calc", RepoPath: fresh, Remote: "origin", CodeBranch: "main"})
	if err != nil || onMain.Branch != "open-architecture/calc" {
		t.Fatalf("restore on main = %+v, %v", onMain, err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", repo, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	onFeature, err := Restore(ctx, RestoreOptions{Cluster: newFakeCluster(), Repository: "calc", RepoPath: other, Remote: "origin", CodeBranch: "spec/bob"})
	if err != nil || onFeature.Branch != "open-architecture/calc--spec-bob" {
		t.Fatalf("restore on the feature branch = %+v, %v", onFeature, err)
	}
}
