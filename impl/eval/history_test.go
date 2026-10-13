package evalrun

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/contextpack"
)

func historyGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func historyWrite(t *testing.T, dir, path, contents string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func historyCommitAll(t *testing.T, dir, message string) {
	t.Helper()
	historyGit(t, dir, "add", "-A")
	historyGit(t, dir, "-c", "user.email=eval@localhost", "-c", "user.name=eval", "commit", "-qm", message)
}

// historyRepo is a checkout of four commits, newest first: one that touches two
// contexts, one that touches no context, and two that touch one each.
func historyRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	historyGit(t, dir, "init", "-q", "-b", "main")
	historyWrite(t, dir, "alpha/one.go", "package alpha\n")
	historyCommitAll(t, dir, "add alpha")
	historyWrite(t, dir, "docs/readme.md", "docs\n")
	historyCommitAll(t, dir, "document alpha")
	historyWrite(t, dir, "alpha/one.go", "package alpha\n\nfunc One() {}\n")
	historyWrite(t, dir, "beta/two.go", "package beta\n")
	historyCommitAll(t, dir, "touch both")
	historyWrite(t, dir, "beta/two.go", "package beta\n\nfunc One() {}\n")
	historyCommitAll(t, dir, "rename the thing")
	return dir
}

// historyOwner is the owner lookup of that checkout: a path belongs to the
// context named after its top directory, and a path outside every context
// belongs to none.
func historyOwner(path string) (string, error) {
	top, _, _ := strings.Cut(filepath.ToSlash(path), "/")
	switch top {
	case "alpha", "beta":
		return top, nil
	}
	return "", nil
}

// historyPack is the pack a query seeds: the query "rename the thing" lands on
// a context that is never gold, and every other query lands on alpha.
func historyPack(_ context.Context, query string) (contextpack.Pack, error) {
	name := "alpha"
	if query == "rename the thing" {
		name = "gamma"
	}
	return contextpack.Pack{
		Seed: contextpack.Seed{Kind: contextpack.SeedQuery, Value: query},
		Neighbors: []contextpack.Card{{
			Title: "out DEPENDS_ON context " + name,
			Name:  name,
			Body:  "intent: " + name + "\n",
		}},
	}, nil
}

func historyRun(t *testing.T) HistoryReport {
	t.Helper()
	report, err := RunHistory(context.Background(), HistoryOptions{
		Repo:    historyRepo(t),
		Commits: 10,
		Owner:   historyOwner,
		Pack:    historyPack,
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func historyRow(t *testing.T, report HistoryReport, query string) CommitReport {
	t.Helper()
	for _, row := range report.Rows {
		if row.Query == query {
			return row
		}
	}
	t.Fatalf("no row for %q in %+v", query, report.Rows)
	return CommitReport{}
}

func TestHistoryWalkPairsTheSubjectWithTheOwnersOfTheChangedFiles(t *testing.T) {
	report := historyRun(t)
	if report.Commits != 4 {
		t.Fatalf("commits = %d, want the four of the checkout", report.Commits)
	}
	both := historyRow(t, report, "touch both")
	if got := strings.Join(both.Gold, ","); got != "alpha,beta" {
		t.Errorf("gold = %q, want both contexts the commit touched", got)
	}
	if both.Hit != 1 || math.Abs(both.Recall-0.5) > 1e-9 {
		t.Errorf("hit/recall = %d/%v, want one of two gold contexts named", both.Hit, both.Recall)
	}
	if both.K != 1 || both.Tokens <= 0 {
		t.Errorf("k/tokens = %d/%d, want the pack's named set and its token count", both.K, both.Tokens)
	}
	if both.Query != "touch both" {
		t.Errorf("query = %q, want the commit subject", both.Query)
	}
	if len(both.Commit) != 40 {
		t.Errorf("commit = %q, want the commit hash", both.Commit)
	}
}

func TestHistorySkipsACommitNoContextOwns(t *testing.T) {
	report := historyRun(t)
	docs := historyRow(t, report, "document alpha")
	if !docs.Skipped {
		t.Fatalf("a commit touching no context was scored: %+v", docs)
	}
	if docs.Recall != 0 || docs.K != 0 {
		t.Errorf("skipped row = %+v, want no recall and no pack", docs)
	}
	if report.Skipped != 1 {
		t.Errorf("skipped = %d, want the one commit no context owns", report.Skipped)
	}
}

func TestHistorySummaryCountsRecallOverTheCommitsWithAGoldSet(t *testing.T) {
	report := historyRun(t)
	if report.Scored != 3 {
		t.Errorf("scored = %d, want the three commits whose files a context owns", report.Scored)
	}
	if math.Abs(report.Recall-0.5) > 1e-9 {
		t.Errorf("recall = %v, want the mean over the scored commits (1, 0.5, 0), not over all four (0.375)", report.Recall)
	}
	if math.Abs(report.Precision-2.0/3.0) > 1e-9 {
		t.Errorf("precision = %v, want the mean over the scored commits", report.Precision)
	}
	if report.Failed != 0 || report.Tokens <= 0 {
		t.Errorf("failed/tokens = %d/%d, want no failures and a mean token count", report.Failed, report.Tokens)
	}
	if encoded, err := report.JSON(); err != nil || !strings.Contains(string(encoded), `"recall": 0.5`) {
		t.Errorf("json = %s, %v, want the summary a second run compares against", encoded, err)
	}
}
