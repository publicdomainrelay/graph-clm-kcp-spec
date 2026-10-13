package evalrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/contextpack"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

// DefaultHistoryCommits is how much of a checkout the git-history benchmark
// walks when the caller names no count: enough commits that the recall number
// is not one author's day, few enough that the pack builder is asked for a
// bounded number of packs.
const DefaultHistoryCommits = 50

// HistoryOptions is one run of the git-history benchmark: a checkout to walk,
// how much of it, and the two lookups the walk needs. The owner lookup answers
// the same question `specctl arch owner` answers for a path, and the pack
// builder answers with the pack a query seeds, so the benchmark measures the
// pack the agent is handed rather than a pack built for the measurement.
type HistoryOptions struct {
	Repo string

	Commits int

	Budget int

	Owner func(path string) (string, error)

	Pack func(ctx context.Context, query string) (contextpack.Pack, error)

	Log *slog.Logger
}

// CommitReport is one commit of the walk: the query the commit's subject line
// asks, the gold contexts that own the files it touched, the contexts the pack
// named, and what the pack cost. Skipped marks a commit no context owns, which
// is not a miss: it has no gold set to miss.
type CommitReport struct {
	Commit string `json:"commit"`

	Query string `json:"query"`

	Gold []string `json:"gold,omitempty"`

	Named []string `json:"named,omitempty"`

	K int `json:"k"`

	Hit int `json:"hit"`

	Recall float64 `json:"recall"`

	Precision float64 `json:"precision"`

	Tokens int `json:"tokens"`

	Dropped int `json:"dropped"`

	Skipped bool `json:"skipped,omitempty"`

	Error string `json:"error,omitempty"`
}

// HistoryReport is the whole walk. Recall and Precision are the means over the
// commits that have a gold set, so a commit no context owns neither helps nor
// hurts the number; Commits counts every commit walked.
type HistoryReport struct {
	Repo string `json:"repo"`

	Commits int `json:"commits"`

	Scored int `json:"scored"`

	Skipped int `json:"skipped"`

	Failed int `json:"failed"`

	Recall float64 `json:"recall"`

	Precision float64 `json:"precision"`

	Tokens int `json:"tokens"`

	Dropped float64 `json:"dropped"`

	StartedAt string `json:"startedAt"`

	Rows []CommitReport `json:"rows,omitempty"`
}

// RunHistory walks a checkout's own commits: for each one the query is the
// commit's subject line and the gold set is the contexts that own the files it
// touched. It builds a pack per query and scores how many gold contexts the
// pack's neighbour and claims sections name. A commit whose files no context
// owns is skipped, because measuring it would report a miss the pack was never
// asked to hit.
func RunHistory(ctx context.Context, options HistoryOptions) (HistoryReport, error) {
	report := HistoryReport{StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if strings.TrimSpace(options.Repo) == "" {
		return report, errors.New("eval: walking a history needs a checkout")
	}
	if options.Owner == nil {
		return report, errors.New("eval: walking a history needs the owner lookup of a path")
	}
	if options.Pack == nil {
		return report, errors.New("eval: walking a history needs a pack builder")
	}
	if options.Commits <= 0 {
		options.Commits = DefaultHistoryCommits
	}
	if options.Log == nil {
		options.Log = logging.Discard()
	}
	report.Repo = options.Repo
	commits, err := gitrepo.Log(ctx, options.Repo, options.Commits)
	if err != nil {
		return report, err
	}
	report.Commits = len(commits)
	recall, precision, tokens, dropped := 0.0, 0.0, 0, 0.0
	for _, commit := range commits {
		row, err := historyCommit(ctx, options, commit)
		if err != nil {
			row.Error = err.Error()
		}
		switch {
		case row.Skipped:
			report.Skipped++
		case row.Error != "":
			report.Failed++
		default:
			report.Scored++
			recall += row.Recall
			precision += row.Precision
			tokens += row.Tokens
			dropped += float64(row.Dropped)
		}
		report.Rows = append(report.Rows, row)
	}
	if report.Scored > 0 {
		report.Recall = recall / float64(report.Scored)
		report.Precision = precision / float64(report.Scored)
		report.Tokens = tokens / report.Scored
		report.Dropped = dropped / float64(report.Scored)
	}
	options.Log.Debug("eval: walked a history", "repo", report.Repo, "commits", report.Commits,
		"scored", report.Scored, "skipped", report.Skipped, "failed", report.Failed, "recall", report.Recall)
	return report, nil
}

func historyCommit(ctx context.Context, options HistoryOptions, commit gitrepo.Commit) (CommitReport, error) {
	row := CommitReport{Commit: commit.Hash, Query: commit.Subject}
	files, err := gitrepo.CommitFiles(ctx, options.Repo, commit.Hash)
	if err != nil {
		return row, err
	}
	gold := []string{}
	seen := map[string]bool{}
	for _, file := range files {
		owner, err := options.Owner(file)
		if err != nil {
			return row, err
		}
		if owner == "" || seen[owner] {
			continue
		}
		seen[owner] = true
		gold = append(gold, owner)
	}
	sort.Strings(gold)
	row.Gold = gold
	if len(gold) == 0 {
		row.Skipped = true
		return row, nil
	}
	pack, err := options.Pack(ctx, commit.Subject)
	if err != nil {
		return row, err
	}
	text, dropped := contextpack.Render(pack, options.Budget)
	row.Named = contextpack.NamedContexts(pack)
	row.K = len(row.Named)
	row.Hit = countNamed(gold, row.Named)
	row.Recall = ratio(row.Hit, len(gold))
	row.Precision = ratio(row.Hit, row.K)
	row.Tokens = contextpack.EstimateTokens(text)
	row.Dropped = len(dropped)
	return row, nil
}

func countNamed(gold, named []string) int {
	held := map[string]bool{}
	for _, name := range gold {
		held[name] = true
	}
	hit := 0
	for _, name := range named {
		if held[name] {
			hit++
		}
	}
	return hit
}

func ratio(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

// Markdown is the printed reading: one line per commit, then the summary. The
// summary is the JSON that lands beside the report, so two runs compare.
func (r HistoryReport) Markdown() string {
	builder := strings.Builder{}
	fmt.Fprintf(&builder, "# git-history benchmark\n\n")
	fmt.Fprintf(&builder, "%s: %d commits\n\n", r.Repo, r.Commits)
	for _, row := range r.Rows {
		builder.WriteString(row.line())
		builder.WriteString("\n")
	}
	fmt.Fprintf(&builder, "\nrecall %.2f precision %.2f tokens %d dropped %.2f over %d scored commit(s): %d skipped (no context owns their files), %d failed\n",
		r.Recall, r.Precision, r.Tokens, r.Dropped, r.Scored, r.Skipped, r.Failed)
	return builder.String()
}

func (r CommitReport) line() string {
	short := r.Commit
	if len(short) > 8 {
		short = short[:8]
	}
	switch {
	case r.Skipped:
		return fmt.Sprintf("- %s skipped (no context owns its files): %s", short, r.Query)
	case r.Error != "":
		return fmt.Sprintf("- %s failed: %s: %s", short, r.Error, r.Query)
	default:
		return fmt.Sprintf("- %s recall %.2f precision %.2f k %d hit %d tokens %d dropped %d: %s",
			short, r.Recall, r.Precision, r.K, r.Hit, r.Tokens, r.Dropped, r.Query)
	}
}

func (r HistoryReport) JSON() ([]byte, error) {
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
