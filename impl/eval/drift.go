package evalrun

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	deltadiff "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/agentfactory"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

// runDrift is goal 5, measured: a human commits a code change, the controller
// raises the code -> spec change for the drift, and the spec the agent writes
// is graded against what a correct spec of the new code would say.
func (h *harness) runDrift(ctx context.Context, fixture Fixture, baselines map[string]baseline) []eval.DriftReport {
	out := make([]eval.DriftReport, 0, len(fixture.Drift))
	for _, scenario := range fixture.Drift {
		out = append(out, h.driftOne(ctx, fixture, scenario, baselines[scenario.Context]))
	}
	return out
}

func (h *harness) driftOne(ctx context.Context, fixture Fixture, scenario DriftScenario, start baseline) (report eval.DriftReport) {
	started := time.Now()
	report = eval.DriftReport{
		Fixture:         fixture.Name,
		Scenario:        scenario.Name,
		Context:         scenario.Context,
		Difficulty:      scenario.Difficulty,
		Description:     scenario.Description,
		ExpectedAdded:   sortedCopy(scenario.ExpectedInterfacesAdded),
		ExpectedRemoved: sortedCopy(scenario.ExpectedInterfacesRemoved),
	}
	defer func() { report.WallTimeSeconds = time.Since(started).Seconds() }()
	deadline, cancel := context.WithTimeout(ctx, h.options.Timeout)
	defer cancel()

	// The code -> spec agent of this run answers the drift. The scripted
	// baseline answers from a draft written beside the fixture, because the
	// fixture's own draft describes the code before the human edit.
	kind := h.summarizeKind
	if isScripted(kind) {
		draftFile, err := h.writeDriftDraft(fixture, scenario)
		if err != nil {
			report.Error = err.Error()
			return report
		}
		kind = agentfactory.Scripted + ":" + draftFile
	}

	h.stopController()
	if err := h.reset(deadline, scenario.Context, "", start); err != nil {
		h.startController()
		report.Error = err.Error()
		return report
	}
	if err := h.setSummarizeAgentKind(deadline, kind); err != nil {
		h.startController()
		report.Error = err.Error()
		return report
	}
	if err := h.startController(); err != nil {
		report.Error = err.Error()
		return report
	}

	if err := h.applyHumanChange(deadline, scenario); err != nil {
		report.Error = err.Error()
		return report
	}
	change, err := h.waitChange(deadline, scenario.Context, specapi.DirectionCodeToSpec)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.ChangePhase = change.Status.Phase
	if change.Status.Phase != specapi.PhaseSucceeded {
		report.Error = strings.TrimSpace(change.Status.Phase + ": " + change.Status.Message + " " + change.Status.AgentLog)
		return report
	}
	after, err := h.context(deadline, scenario.Context)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	entries, delta := eval.ScoreInterfaceDelta(deltadiff.Diff(start.spec, after.Spec), scenario.ExpectedDeltaEntries)
	report.Delta = delta
	report.InterfacesAdded = entries.Added
	report.InterfacesRemoved = entries.Removed
	if len(scenario.Prose) > 0 {
		result, err := h.judge.Judge(deadline, eval.JudgeRequest{
			Context: scenario.Context,
			Facts:   scenario.Prose,
			Spec:    eval.SpecProse(after.Spec),
		})
		if err != nil {
			report.Error = err.Error()
			return report
		}
		report.ProseScore = eval.ScoreFacts(fixture.Name, scenario.Context, scenario.Prose, result, h.judgeName)
	}
	report.Pass = sameSet(entries.Added, scenario.ExpectedInterfacesAdded) &&
		sameSet(entries.Removed, scenario.ExpectedInterfacesRemoved) &&
		delta.Precise &&
		report.ProseScore.Stated == report.ProseScore.Expected

	// Put the tree and the spec back, so the next drift or scenario starts from
	// the baseline and not from this one's result. The controller stays stopped
	// across the reset.
	h.stopController()
	if err := h.reset(ctx, scenario.Context, "", start); err != nil {
		report.Error = err.Error()
	}
	h.startController()
	return report
}

// applyHumanChange writes the scenario's code change into the tree and commits
// it the way a person commits one. Nothing about the commit is the tool's: the
// controller sees a new HEAD and raises code -> spec for it.
func (h *harness) applyHumanChange(ctx context.Context, scenario DriftScenario) error {
	patcher := scriptedagent.New(&scriptedagent.Scenario{
		Realize: map[string][]scriptedagent.Step{scenario.Context: scenario.Commit},
	})
	if _, err := patcher.Realize(ctx, agent.RealizeRequest{Context: scenario.Context, Dir: h.dir}); err != nil {
		return err
	}
	if err := h.git(ctx, "add", "-A"); err != nil {
		return err
	}
	return h.git(ctx, "-c", "user.name=human", "-c", "user.email=human@localhost",
		"commit", "-qm", "human: "+scenario.Name)
}

// writeDriftDraft writes the scripted code -> spec answer for one drift
// scenario, outside the working tree so the agent under test cannot read the
// intended spec out of the tree it is reading.
func (h *harness) writeDriftDraft(fixture Fixture, scenario DriftScenario) (string, error) {
	if err := os.MkdirAll(h.scratch, 0o755); err != nil {
		return "", err
	}
	encoded, err := yaml.Marshal(scriptedagent.Scenario{
		Contexts: map[string]scriptedagent.Draft{scenario.Context: scenario.Draft},
	})
	if err != nil {
		return "", err
	}
	path := filepath.Join(h.scratch, fixture.Name+"-drift-"+scenario.Name+".yaml")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// setSummarizeAgentKind points both agent selections of the Repository at the
// kind the drift's code -> spec pass must use. The summarize reconciler reads
// the repository's populate agent, so naming only spec.agent would leave the
// fixture's own draft answering the drift.
func (h *harness) setSummarizeAgentKind(ctx context.Context, kind string) error {
	repository, err := h.typedRepository(ctx)
	if err != nil {
		return err
	}
	selection := &spec.AgentSpec{Kind: kind}
	repository.Spec.Agent = selection
	if repository.Spec.Populate != nil {
		repository.Spec.Populate.Agent = selection
	}
	return applyTyped(ctx, h.client, repository)
}

func sortedCopy(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

// sameSet compares two name lists as sets.
func sameSet(left, right []string) bool {
	leftSet, rightSet := map[string]bool{}, map[string]bool{}
	for _, name := range left {
		if name != "" {
			leftSet[name] = true
		}
	}
	for _, name := range right {
		if name != "" {
			rightSet[name] = true
		}
	}
	if len(leftSet) != len(rightSet) {
		return false
	}
	for name := range leftSet {
		if !rightSet[name] {
			return false
		}
	}
	return true
}
