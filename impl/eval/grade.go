package evalrun

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	deltadiff "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	impljudge "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/judge"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/stripbodies"
)

// buildJudge picks how behavioural facts are graded: the deterministic keyword
// judge under the scripted baseline, a model judge otherwise. A judge failure
// is a harness failure and is reported as one, never scored as the model's.
func (h *harness) buildJudge(summarizeKind string) eval.Judge {
	if h.options.Judge != nil {
		h.judgeName = "custom"
		return h.options.Judge
	}
	if isScripted(summarizeKind) {
		h.judgeName = "keyword"
		return eval.KeywordJudge{}
	}
	h.judgeName = "model"
	return impljudge.New(impljudge.Options{
		Command: h.options.JudgeCommand,
		Args:    h.options.JudgeArgs,
		Dir:     h.dir,
		Timeout: h.options.AgentTimeout,
		Env:     h.agentEnv(),
	})
}

// gradeFacts scores each context's spec against the behavioural facts a correct
// spec must state. This is the code -> spec measure that means something: the
// observed interface list is in the bundle, so recall and precision say what
// the prompt said, and only the prose can say what the model read.
func (h *harness) gradeFacts(ctx context.Context, fixture Fixture, contexts []spec.SystemContext) []eval.FactsReport {
	out := make([]eval.FactsReport, 0, len(contexts))
	for _, entry := range contexts {
		expected := fixture.Expectations.Contexts[entry.Name].Facts
		if len(expected) == 0 {
			out = append(out, eval.FactsReport{Fixture: fixture.Name, Context: entry.Name})
			continue
		}
		result, err := h.judge.Judge(ctx, eval.JudgeRequest{
			Context: entry.Name,
			Facts:   expected,
			Spec:    eval.SpecProse(entry.Spec),
		})
		if err != nil {
			h.options.Log.Warn("eval: the fact judge failed", "context", entry.Name, "err", err)
			report := eval.ScoreFacts(fixture.Name, entry.Name, expected, eval.JudgeResult{}, h.judgeName)
			report.Error = err.Error()
			out = append(out, report)
			continue
		}
		out = append(out, eval.ScoreFacts(fixture.Name, entry.Name, expected, result, h.judgeName))
	}
	return out
}

// measureSufficiency is the strongest test of a spec the harness has: remove a
// context's implementation bodies, keep its spec, let the model rebuild the
// code from the spec alone, and run the tests it never saw.
func (h *harness) measureSufficiency(ctx context.Context, fixture Fixture, contexts []spec.SystemContext, baseCommit string) []eval.SufficiencyReport {
	out := make([]eval.SufficiencyReport, 0, len(contexts))
	for _, entry := range contexts {
		out = append(out, h.suffice(ctx, fixture, entry, baseCommit))
	}
	return out
}

func (h *harness) suffice(ctx context.Context, fixture Fixture, entry spec.SystemContext, baseCommit string) (report eval.SufficiencyReport) {
	started := time.Now()
	report = eval.SufficiencyReport{
		Fixture: fixture.Name,
		Context: entry.Name,
		Agent:   orScripted(h.options.Agent),
	}
	defer func() { report.WallTimeSeconds = time.Since(started).Seconds() }()
	files := entry.Status.Observed.Files
	report.Files = len(files)
	if len(files) == 0 {
		report.Skipped = true
		return report
	}

	// One context's sufficiency is bounded like one scenario: the model call
	// has its own timeout, but the test command that grades the rebuild does
	// not, and a test runner that hangs must not hang the whole run.
	ctx, cancel := context.WithTimeout(ctx, h.options.Timeout)
	defer cancel()

	// The controller is stopped: the tree is about to lose its bodies and the
	// tests are about to disappear, and neither is a change anyone should
	// reconcile.
	h.stopController()
	defer h.startController()
	if err := h.resetTree(ctx, baseCommit); err != nil {
		report.Error = err.Error()
		return report
	}
	hidden, err := hideTests(h.dir)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	defer func() { _ = hidden.restore() }()

	stripped, err := stripbodies.Strip(h.dir, files)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.Stripped = stripped.Stripped
	if stripped.Stripped == 0 {
		report.Skipped = true
		return report
	}

	rebuild, err := h.agent(h.realizeKind)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	result, err := rebuild.Realize(ctx, agent.RealizeRequest{
		Context:    entry.Name,
		Repository: h.repository.Name,
		Dir:        h.dir,
		Delta:      deltadiff.Diff(spec.SystemContextSpec{}, entry.Spec),
		ToSpec:     entry.Spec,
		Observed:   entry.Status.Observed,
		Verify:     fixture.Config.Accept,
		Instruction: "The implementation bodies in this tree were removed and every test file was " +
			"hidden. Rebuild the code so it satisfies the specification: change implementation bodies " +
			"only and keep every signature.",
	})
	report.FilesTouched = result.Files
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.Realized = true

	if err := hidden.restore(); err != nil {
		report.Error = err.Error()
		return report
	}
	pass, err := runCommand(ctx, fixture.Config.Accept, h.dir)
	report.TestsPass = pass
	if err != nil {
		report.Error = err.Error()
	}
	return report
}

// hiddenTests is the test files a sufficiency run took out of the tree, held so
// they can be put back byte for byte. The model must not read them, and the
// grade must run them.
type hiddenTests struct {
	files map[string][]byte
}

func hideTests(dir string) (*hiddenTests, error) {
	hidden := &hiddenTests{files: map[string][]byte{}}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".codegraph", "node_modules", ".specs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") && !strings.HasSuffix(entry.Name(), "_test.ts") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hidden.files[path] = contents
		return os.Remove(path)
	})
	if err != nil {
		return hidden, fmt.Errorf("eval: hide the tests: %w", err)
	}
	return hidden, nil
}

func (h *hiddenTests) restore() error {
	for path, contents := range h.files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, contents, 0o644); err != nil {
			return err
		}
		delete(h.files, path)
	}
	return nil
}

// resetTree puts the working tree back on the baseline commit with nothing
// left over. It is the tree half of reset, split out because the sufficiency
// measure uses it without touching the spec. A codebase the controller cloned
// has no commit the harness took, so its own HEAD is the baseline.
func (h *harness) resetTree(ctx context.Context, commit string) error {
	if commit == "" {
		commit = "HEAD"
	}
	if err := h.git(ctx, "reset", "--hard", commit); err != nil {
		return err
	}
	if err := h.git(ctx, "clean", "-fdq"); err != nil {
		return err
	}
	return h.deleteBranches(ctx)
}

// runCommand runs one gate command in the tree and reports whether it exited
// zero.
func runCommand(ctx context.Context, command []string, dir string) (bool, error) {
	if len(command) == 0 {
		return true, nil
	}
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Dir = dir
	output, err := process.CombinedOutput()
	if err == nil {
		return true, nil
	}
	return false, fmt.Errorf("the command %s failed: %v: %s", strings.Join(command, " "), err, tail(string(output)))
}
