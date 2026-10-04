package evalrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/agentfactory"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/summarize"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const (
	// DefaultTimeout bounds one scenario from the spec edit to a settled change.
	DefaultTimeout = 5 * time.Minute

	// DefaultScenarioAttempts is how many attempts one scenario's change gets
	// before the harness gives up on it. It is small on purpose: an eval run
	// measures the loop, it does not retry it forever.
	DefaultScenarioAttempts = 2

	defaultResync = 500 * time.Millisecond

	defaultRetryBackoff = time.Second

	pollInterval = 250 * time.Millisecond

	// scenarioManager is the field manager the scenario edit is applied under.
	// It is distinct from the tool's own writer, so the controller reads the
	// edit as the human change it stands in for.
	scenarioManager = "specctl-eval-scenario"

	gitAuthorName = "eval"

	gitAuthorMail = "eval@localhost"
)

// Report is what one run measured. It is the pure report of abc/eval, named
// here so a caller of this package needs one import and not two.
type Report = eval.Report

// Comparison is a live run beside the scripted baseline, and the two functions
// that produce it, re-exported for the same reason as Report.
type Comparison = eval.Comparison

func ParseReport(encoded []byte) (eval.Report, error) { return eval.ParseReport(encoded) }

func Compare(baseline, live eval.Report) eval.Comparison { return eval.Compare(baseline, live) }

// Options is one eval run.
type Options struct {
	FixturesDir string

	ScenarioGlob string

	// Agent is the agent kind of both halves, in the agentfactory spelling:
	// claude, claude-mod or pi. Empty means the scripted baseline, where each
	// fixture answers from its own files.
	Agent string

	// SummarizeAgent, when set, is the kind of the code -> spec half only, so a
	// run can measure one host in that direction and another in the other.
	SummarizeAgent string

	AgentCommand string

	AgentArgs []string

	AgentTimeout time.Duration

	ClmMod string

	PiExtension string

	Kubeconfig string

	Context string

	Workspace string

	Namespace string

	QPS float32

	Burst int

	Client *kcpclient.Client

	WorkDir string

	Keep bool

	// CodeOnly stops after the code -> spec half and the round trip: a run that
	// only asks whether one host can read a codebase.
	CodeOnly bool

	// NoRoundTrip skips the second and third summarize of every context. The
	// round trip is two more model calls per context, which is worth paying for
	// a fixture and not for a codebase of forty of them.
	NoRoundTrip bool

	// SkipSuffice drops the spec sufficiency measure, which is one model call
	// and one test run per context. It is off by default because it is the
	// strongest test of a spec there is.
	SkipSuffice bool

	// Judge overrides how facts are graded. Empty means the keyword judge under
	// the scripted baseline and a model judge otherwise.
	Judge eval.Judge

	JudgeCommand string

	JudgeArgs []string

	Timeout time.Duration

	MaxAttempts int

	Tool string

	Writer graph.Writer

	Log *slog.Logger
}

// baseline is where every scenario of one context starts: the commit the tree
// is reset to, the spec the code -> spec half left, and the fingerprint of the
// facts that spec was written about.
type baseline struct {
	commit string

	spec spec.SystemContextSpec

	observed spec.ObservedFacts
}

type harness struct {
	options Options

	client *kcpclient.Client

	namespace string

	dir string

	repository *spec.Repository

	// realizeKind and summarizeKind are the agent kinds of the two halves, fixed
	// once per fixture so the sufficiency measure, a drift scenario and a CLM
	// scenario can tell a live host from the scripted baseline.
	realizeKind string

	summarizeKind string

	// scratch holds the generated scenario files, outside every working tree.
	scratch string

	// judge grades the behavioural facts a spec must state.
	judge eval.Judge

	judgeName string

	// stop stops the in-process controller. A scenario stops it across the
	// reset, so nothing reconciles the reverted tree while it is being put
	// back, and starts it again before the scenario's edit is applied.
	stop func()
}

// Run drives every fixture through both halves of the loop and returns the
// numbers. The caller owns the workspace and the client; Run owns the working
// trees and, unless Keep is set, the objects it created.
func Run(ctx context.Context, options Options) (eval.Report, error) {
	report := eval.Report{
		Agent:     orScripted(options.Agent),
		Fixtures:  options.FixturesDir,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if options.Client == nil {
		return report, errors.New("eval: a cluster client is required")
	}
	if options.Namespace == "" {
		options.Namespace = specapi.DefaultNamespace
	}
	if options.Timeout <= 0 {
		options.Timeout = DefaultTimeout
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = DefaultScenarioAttempts
	}
	if options.Log == nil {
		options.Log = logging.Discard()
	}
	// A host inside the model runs in a worktree, so every path it is handed
	// has to be absolute: a relative kubeconfig would be resolved against the
	// directory the model happens to be editing.
	if options.Kubeconfig != "" {
		if absolute, err := filepath.Abs(options.Kubeconfig); err == nil {
			options.Kubeconfig = absolute
		}
	}
	if err := options.Client.Ping(ctx); err != nil {
		return report, err
	}
	fixtures, err := Load(options.FixturesDir)
	if err != nil {
		return report, err
	}
	fixtures, err = SelectScenarios(fixtures, options.ScenarioGlob)
	if err != nil {
		return report, err
	}
	for _, fixture := range fixtures {
		if fixture.Config.Source != nil && options.Agent == "" && options.SummarizeAgent == "" {
			return report, fmt.Errorf("eval: %s is a codebase with no scripted answer, so it needs a named agent", fixture.Name)
		}
	}
	workDir := options.WorkDir
	if workDir == "" {
		workDir, err = os.MkdirTemp("", "specd-eval.")
		if err != nil {
			return report, fmt.Errorf("eval: make a working directory: %w", err)
		}
		if !options.Keep {
			defer os.RemoveAll(workDir)
		}
	}
	report.Notes = append(report.Notes, "working trees under "+workDir)

	for _, fixture := range fixtures {
		run, err := runFixture(ctx, options, fixture, workDir)
		report.Populate = append(report.Populate, run.populate)
		report.CodeToSpec = append(report.CodeToSpec, run.codeToSpec...)
		report.Facts = append(report.Facts, run.facts...)
		report.Sufficiency = append(report.Sufficiency, run.sufficiency...)
		report.Drift = append(report.Drift, run.drift...)
		report.Scenarios = append(report.Scenarios, run.scenarios...)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("fixture %s stopped early: %v", fixture.Name, err))
			report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			return report, nil
		}
	}
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return report, nil
}

type fixtureRun struct {
	populate eval.PopulateReport

	codeToSpec []eval.CodeToSpecReport

	facts []eval.FactsReport

	sufficiency []eval.SufficiencyReport

	drift []eval.DriftReport

	scenarios []eval.ScenarioReport
}

func runFixture(ctx context.Context, options Options, fixture Fixture, workDir string) (fixtureRun, error) {
	run := fixtureRun{populate: eval.PopulateReport{Fixture: fixture.Name, Phase: "NotStarted"}}
	client := options.Client
	dir := filepath.Join(workDir, fixture.Name)
	baseCommit := ""
	cloned := fixture.Config.Source != nil
	if cloned {
		// A git source is resolved by the controller into its own cache, so
		// nothing is copied and nothing is written where the source lives.
		dir = ""
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return run, err
		}
		if err := CopyTree(fixture.Dir, dir); err != nil {
			return run, err
		}
		commit, err := initRepository(ctx, dir)
		if err != nil {
			return run, err
		}
		baseCommit = commit
	}
	if err := forget(ctx, client, options.Namespace, fixture.Name); err != nil {
		return run, err
	}
	scenarioFiles, err := writeScenarioFiles(workDir, fixture)
	if err != nil {
		return run, err
	}

	summarizeKind, realizeKind := fixture.kinds(options, scenarioFiles)
	populate := &spec.RepositoryPopulate{
		Partition: spec.PartitionDirectory,
		Summarize: true,
		Agent:     &spec.AgentSpec{Kind: summarizeKind},
	}
	if extra := fixture.Config.Populate; extra != nil {
		if extra.Partition != "" {
			populate.Partition = extra.Partition
		}
		populate.Include = extra.Include
		populate.Exclude = extra.Exclude
	}
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: fixture.Name, Namespace: options.Namespace},
		Spec: spec.RepositorySpec{
			Branch:   "main",
			Verify:   fixture.Config.Verify,
			Agent:    &spec.AgentSpec{Kind: realizeKind},
			Populate: populate,
		},
	}
	if cloned {
		repository.Spec.Source = &spec.RepositorySource{
			Git: &spec.GitSource{URL: fixture.Config.Source.URL, Ref: fixture.Config.Source.Ref},
		}
	} else {
		repository.Spec.Path = dir
	}
	if err := applyTyped(ctx, client, repository); err != nil {
		return run, err
	}

	h := &harness{
		options:       options,
		client:        client,
		namespace:     options.Namespace,
		dir:           dir,
		repository:    repository,
		realizeKind:   realizeKind,
		summarizeKind: summarizeKind,
		scratch:       filepath.Join(workDir, "eval-drift"),
	}
	h.judge = h.buildJudge(summarizeKind)
	if err := h.startController(); err != nil {
		return run, err
	}
	defer func() {
		h.stopController()
		if options.Keep {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = forget(cleanupCtx, client, options.Namespace, fixture.Name)
	}()

	// The code -> spec half: one Repository manifest, every context summarized,
	// measured from the objects the controller wrote.
	populateStart := time.Now()
	waitErr := h.waitPopulated(ctx)
	run.populate.WallTimeSeconds = time.Since(populateStart).Seconds()
	status, statusErr := h.repositoryStatus(ctx)
	if statusErr != nil {
		run.populate.Error = statusErr.Error()
		return run, nil
	}
	run.populate.Phase = status.Phase
	if status.Contexts != nil {
		run.populate.Contexts = status.Contexts.Total
		run.populate.Summarized = status.Contexts.Summarized
		run.populate.Failed = status.Contexts.Failed
	}
	if waitErr != nil {
		run.populate.Error = waitErr.Error()
		return run, nil
	}
	if status.Phase != specapi.PhasePopulated {
		run.populate.Error = populateError(status)
		run.populate.Failures = h.failedSummaries(ctx)
		// A codebase that only partly populated is still a measurement: what
		// was summarized is measured, and the rest is reported by name.
		if status.Contexts == nil || status.Contexts.Summarized == 0 {
			return run, nil
		}
	}
	// A git source is resolved into the controller's cache, so the tree the
	// measurements read is the one the Repository reports, never a path this
	// harness guessed.
	if status.ResolvedPath != "" {
		h.dir = status.ResolvedPath
	}

	contexts, err := h.contexts(ctx)
	if err != nil {
		return run, err
	}
	summarizer, err := h.agent(summarizeKind)
	if err != nil {
		return run, err
	}
	run.codeToSpec = h.measureCodeToSpec(ctx, contexts, summarizer)
	run.facts = h.gradeFacts(ctx, fixture, contexts)
	if !options.SkipSuffice && !isScripted(h.realizeKind) {
		run.sufficiency = h.measureSufficiency(ctx, fixture, contexts, baseCommit)
	}

	// Every scenario starts from the same baseline, so the fixtures are compared
	// with each other and not with their own history. The baseline is read
	// before the drift scenarios run, because a drift scenario changes the spec
	// and the change must not become the next scenario's starting point.
	baselines, err := h.baselines(ctx, baseCommit)
	if err != nil {
		return run, err
	}
	if len(fixture.Drift) > 0 {
		run.drift = h.runDrift(ctx, fixture, baselines)
	}

	// The spec -> code half.
	if options.CodeOnly || len(fixture.Scenarios) == 0 {
		return run, nil
	}
	for _, scenario := range fixture.Scenarios {
		run.scenarios = append(run.scenarios, h.runScenario(ctx, fixture, scenario, scenarioFiles[scenario.Name], baselines))
	}
	return run, nil
}

// isScripted reports whether an agent kind answers from a scenario file, which
// is the deterministic baseline and the only agent that cannot rebuild code
// from a spec or edit a context document for a reason.
func isScripted(kind string) bool {
	return kind == "" || strings.HasPrefix(kind, agentfactory.Scripted+":")
}

// kinds is the agent of each half. An empty Agent is the scripted baseline: the
// code -> spec half answers from the fixture's own drafts and the spec -> code
// half from the generated scenario file. SummarizeAgent overrides the code ->
// spec half alone, and the bare word scripted means the fixture's own drafts,
// because the drafts live beside the fixture and the caller cannot name them.
func (f Fixture) kinds(options Options, scenarioFiles map[string]string) (string, string) {
	drafts := "scripted:" + filepath.Join(f.Dir, fixtureDrafts)
	realize := drafts
	for _, scenario := range f.Scenarios {
		realize = "scripted:" + scenarioFiles[scenario.Name]
		break
	}
	summarize := drafts
	if options.Agent != "" {
		summarize = options.Agent
		realize = options.Agent
	}
	switch options.SummarizeAgent {
	case "":
	case agentfactory.Scripted:
		summarize = drafts
	default:
		summarize = options.SummarizeAgent
	}
	return summarize, realize
}

// startController starts the controller the whole loop runs against, and
// stopController stops it. The two are called around every reset.
func (h *harness) startController() error {
	if h.stop != nil {
		return nil
	}
	stop, err := h.newController()
	if err != nil {
		return err
	}
	h.stop = stop
	return nil
}

func (h *harness) stopController() {
	if h.stop == nil {
		return
	}
	h.stop()
	h.stop = nil
}

func (h *harness) newController() (func(), error) {
	controller, err := specd.New(specd.Options{
		Kubeconfig:   h.options.Kubeconfig,
		Context:      h.options.Context,
		Workspace:    h.options.Workspace,
		Namespace:    h.namespace,
		QPS:          h.options.QPS,
		Burst:        h.options.Burst,
		Watch:        specd.WatchInformer,
		Resync:       defaultResync,
		MaxAttempts:  h.options.MaxAttempts,
		RetryBackoff: defaultRetryBackoff,
		Tool:         h.options.Tool,
		Graph:        h.options.Writer,
		AgentCommand: h.options.AgentCommand,
		AgentArgs:    h.options.AgentArgs,
		AgentTimeout: h.options.AgentTimeout,
		ClmMod:       h.options.ClmMod,
		PiExtension:  h.options.PiExtension,
		AgentEnv:     h.agentEnv(),
		Log:          h.options.Log,
	})
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- controller.Run(runCtx) }()
	return func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(60 * time.Second):
		}
	}, nil
}

// agentEnv is what a host inside the model reads to reach the same state the
// controller watches: the workspace, the state bridge and the namespace. A
// plain model ignores all of it.
func (h *harness) agentEnv() map[string]string {
	return map[string]string{
		"SPECD_KUBECONFIG": h.options.Kubeconfig,
		"KUBECONFIG":       h.options.Kubeconfig,
		"SPECD_WORKSPACE":  h.options.Workspace,
		"SPECD_NAMESPACE":  h.namespace,
		"SPECD_SPECCTL":    specctlPath(),
		// A host inside the model keeps the context document under the working
		// tree and applies it with specctl, so it needs the tree it owns.
		"SPECD_CLM_REPO": h.dir,
	}
}

// specctlPath is the state bridge a host inside the model calls. The eval
// harness is normally run by specctl itself, so its own executable is the
// bridge; SPECD_SPECCTL overrides it and a specctl on PATH is the fallback.
func specctlPath() string {
	if fromEnv := os.Getenv("SPECD_SPECCTL"); fromEnv != "" {
		return fromEnv
	}
	if executable, err := os.Executable(); err == nil && filepath.Base(executable) == "specctl" {
		return executable
	}
	return "specctl"
}

// agent builds the agent of one half explicitly. It goes through AgentFor and
// not Agent because a Repository that names its own agent wins over the
// controller's, which would silently answer a round trip asked for in the
// code -> spec half with the agent of the other half.
func (h *harness) agent(kind string) (agent.Agent, error) {
	factory, err := agentfactory.New(agentfactory.Options{
		Kind:        kind,
		Command:     h.options.AgentCommand,
		Args:        h.options.AgentArgs,
		Timeout:     h.options.AgentTimeout,
		ClmMod:      h.options.ClmMod,
		PiExtension: h.options.PiExtension,
		Env:         h.agentEnv(),
	})
	if err != nil {
		return nil, err
	}
	return factory.AgentFor(&spec.AgentSpec{Kind: kind}, h.repository, h.dir)
}

// measureCodeToSpec scores the spec the code -> spec half wrote against the
// facts the index observed, and then summarizes every context twice more to see
// whether a second pass says the same thing.
func (h *harness) measureCodeToSpec(ctx context.Context, contexts []spec.SystemContext, summarizer agent.Agent) []eval.CodeToSpecReport {
	out := make([]eval.CodeToSpecReport, 0, len(contexts))
	for _, entry := range contexts {
		report := eval.CodeToSpecReport{
			Fixture:        h.repository.Name,
			Context:        entry.Name,
			Score:          eval.InterfaceScore(entry.Spec.Interfaces, entry.Status.Observed),
			Requirements:   len(entry.Spec.Requirements),
			AnchoringRate:  eval.AnchoringRate(entry.Spec.Requirements, entry.Status.Observed),
			ValidatorPass:  eval.ValidatorPass(entry.Name, entry.Spec),
			Empty:          report0IsEmpty(entry),
			NoRequirements: len(entry.Spec.Requirements) == 0,
		}
		if h.options.NoRoundTrip {
			out = append(out, report)
			continue
		}
		first, err := h.summarizeOnce(ctx, entry.Name, summarizer)
		if err != nil {
			h.options.Log.Warn("eval: the first round trip summarize failed", "context", entry.Name, "err", err)
			out = append(out, report)
			continue
		}
		second, err := h.summarizeOnce(ctx, entry.Name, summarizer)
		if err != nil {
			h.options.Log.Warn("eval: the second round trip summarize failed", "context", entry.Name, "err", err)
			out = append(out, report)
			continue
		}
		report.DroppedRefs = len(first.Dropped)
		report.Score = eval.InterfaceScore(first.Draft.Interfaces, entry.Status.Observed)
		report.AnchoringRate = eval.AnchoringRate(first.Draft.Requirements, entry.Status.Observed)
		report.NoRequirements = len(first.Draft.Requirements) == 0
		report.RoundTripJaccard = eval.Jaccard(interfaceNames(first.Draft.Interfaces), interfaceNames(second.Draft.Interfaces))
		report.RoundTripMeasured = true
		report.RequirementCountDelta = len(second.Draft.Requirements) - len(first.Draft.Requirements)
		out = append(out, report)
	}
	return out
}

// report0IsEmpty marks the contexts that declare nothing and observe nothing.
// They are left out of the means and counted instead: a context with no surface
// scores a perfect 1 by definition, and averaging those in is how a run of
// empty contexts reports 100%.
func report0IsEmpty(entry spec.SystemContext) bool {
	return len(entry.Spec.Interfaces) == 0 && len(entry.Status.Observed.Interfaces) == 0
}

func (h *harness) summarizeOnce(ctx context.Context, name string, summarizer agent.Agent) (summarize.Result, error) {
	copied := *h.repository
	copied.Status.ResolvedPath = h.dir
	return summarize.Run(ctx, summarize.Options{
		Cluster:    h.client,
		Namespace:  h.namespace,
		Context:    name,
		Repository: &copied,
		Agent:      summarizer,
		Writer:     h.options.Writer,
		Codegraph:  codegraphcli.Runner{Tool: h.options.Tool, Dir: h.dir},
	})
}

// baselines reads where every context of the fixture starts: the spec the code
// -> spec half left, and the fingerprint of the facts it was written about.
func (h *harness) baselines(ctx context.Context, commit string) (map[string]baseline, error) {
	contexts, err := h.contexts(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]baseline{}
	for _, entry := range contexts {
		out[entry.Name] = baseline{commit: commit, spec: entry.Spec, observed: entry.Status.Observed}
	}
	return out, nil
}

// runScenario resets the tree and the spec to the baseline, applies the
// scenario's edit, waits for the controller to work it off, and grades the
// result with the hidden acceptance tests.
func (h *harness) runScenario(ctx context.Context, fixture Fixture, scenario Scenario, scenarioFile string, starts map[string]baseline) eval.ScenarioReport {
	started := time.Now()
	report := eval.ScenarioReport{
		Fixture:     fixture.Name,
		Scenario:    scenario.Name,
		Context:     scenario.Context,
		Difficulty:  scenario.Difficulty,
		Description: scenario.Description,
		Agent:       orScripted(h.options.Agent),
		Via:         scenario.Via,
		Request:     scenario.Request,
	}
	targets := scenario.resolvedTargets()
	if len(targets) > 1 {
		names := make([]string, 0, len(targets))
		for _, target := range targets {
			names = append(names, target.Context)
		}
		report.Context = strings.Join(names, "+")
	}
	deadline, cancel := context.WithTimeout(ctx, h.options.Timeout)
	defer cancel()

	// A CLM scenario is the model's work: it edits the context document and the
	// host inside it applies the delta. The scripted baseline has no host in it,
	// so the scenario is left out of the run and counted as skipped rather than
	// scored as a failure.
	if scenario.Via == "clm" && isScripted(h.realizeKind) {
		report.Skipped = true
		report.WallTimeSeconds = time.Since(started).Seconds()
		return report
	}

	report.Pass = true
	report.VerifyPass = true
	report.AcceptancePass = true
	report.Delta.Precise = true
	entries, expected := 0, 0
	for _, target := range targets {
		report.Delta.Expected += target.ExpectedDeltaEntries
		one, err := h.runTarget(deadline, fixture, scenario, scenarioFile, target, starts[target.Context])
		entries += one.Delta.Entries
		report.Attempts += one.Attempts
		report.ProgressRecords += one.ProgressRecords
		report.FilesOutside = append(report.FilesOutside, one.FilesOutside...)
		report.VerifyPass = report.VerifyPass && one.VerifyPass
		report.AcceptancePass = report.AcceptancePass && one.AcceptancePass
		report.Delta.Precise = report.Delta.Precise && one.Delta.Precise
		if err != nil {
			report.Pass = false
			if report.Error == "" {
				report.Error = target.Context + ": " + err.Error()
			}
		}
	}
	expected = report.Delta.Expected
	report.Delta.Entries = entries
	report.Delta.Precise = report.Delta.Precise && entries == expected
	report.WallTimeSeconds = time.Since(started).Seconds()
	return report
}

// runTarget is one context of a scenario: reset that context and the tree,
// apply the edit or drive the model, wait for the change, and grade it.
func (h *harness) runTarget(ctx context.Context, fixture Fixture, scenario Scenario, scenarioFile string, target Target, start baseline) (eval.ScenarioReport, error) {
	report := eval.ScenarioReport{Context: target.Context}

	// The controller is stopped across the reset: the tree goes back to the
	// baseline commit, and a reconcile of that window would read the revert
	// against the last scenario's spec.
	h.stopController()
	if err := h.reset(ctx, target.Context, scenarioFile, start); err != nil {
		h.startController()
		return report, err
	}
	if err := h.startController(); err != nil {
		return report, err
	}
	before, err := h.context(ctx, target.Context)
	if err != nil {
		return report, err
	}
	if scenario.Via == "clm" {
		if err := h.driveClm(ctx, scenario); err != nil {
			return report, err
		}
	} else if err := h.applyPatch(ctx, target.Context, target.SpecPatch); err != nil {
		return report, err
	}

	change, err := h.waitChange(ctx, target.Context, specapi.DirectionSpecToCode)
	if err != nil {
		return report, err
	}
	report.Attempts = h.changeCount(ctx, target.Context)
	report.ProgressRecords = len(change.Status.Progress)
	report.Delta = eval.ScoreDelta(change.Spec.Delta, target.ExpectedDeltaEntries)
	report.FilesOutside = eval.FilesOutsideContext(change.Status.FilesTouched, before.Status.Observed)
	report.VerifyPass = change.Status.Phase == specapi.PhaseSucceeded && change.Status.VerifyExitCode == 0
	if change.Status.Phase != specapi.PhaseSucceeded {
		return report, fmt.Errorf("%s", strings.TrimSpace(change.Status.Phase+": "+change.Status.Message+" "+change.Status.AgentLog))
	}

	pass, err := h.runAcceptance(ctx, fixture.Config.Accept, target.Acceptance)
	report.AcceptancePass = pass
	if err != nil {
		return report, err
	}
	missing := h.waitInterfaces(ctx, target.Context, target.ExpectedInterfaces, target.ExpectedRemovedInterfaces)
	if len(missing) > 0 {
		return report, fmt.Errorf("the observed surface is wrong: %s", strings.Join(missing, ", "))
	}
	return report, nil
}

// reset puts the working tree and the context back where the scenario found
// them: the tree at the baseline commit, the spec at the baseline every
// scenario starts from, and no change left over from the scenario before.
func (h *harness) reset(ctx context.Context, name, scenarioFile string, start baseline) error {
	if err := h.git(ctx, "reset", "--hard", start.commit); err != nil {
		return err
	}
	if err := h.git(ctx, "clean", "-fdq"); err != nil {
		return err
	}
	if err := h.deleteBranches(ctx); err != nil {
		return err
	}
	if err := h.setAgentKind(ctx, scenarioFile); err != nil {
		return err
	}
	current, err := h.context(ctx, name)
	if err != nil {
		return err
	}
	if err := h.restoreSpec(ctx, current, start); err != nil {
		return err
	}
	if err := h.deleteChanges(ctx, name); err != nil {
		return err
	}
	return h.waitNoChanges(ctx, name)
}

// waitNoChanges deletes every change of the context until the workspace holds
// none, so the change the scenario raises is the only one admission can see. It
// deletes inside the wait because a reconcile that was already running can put
// one back between the delete and the read.
func (h *harness) waitNoChanges(ctx context.Context, name string) error {
	return h.wait(ctx, "the changes of "+name+" to be swept", func() bool {
		changes, err := h.changes(ctx, name)
		if err != nil {
			return false
		}
		if len(changes) == 0 {
			return true
		}
		for _, change := range changes {
			if err := h.client.Delete(ctx, specapi.SpecChangeGVR, h.namespace, change.Name); err != nil {
				return false
			}
		}
		return false
	})
}

// restoreSpec writes the baseline back as one settled context: the baseline
// spec with the tool's own origin annotation, so the write is never read as a
// human edit, and the facts of the baseline commit as the observed *and* the
// synced state, so the revert the harness just made in git is not reported as
// drift. It is a whole object write because the spec it replaces may hold
// entries a scenario added.
//
// The facts are written rather than waited for on purpose: an ingest that ran
// between the git reset and this write would see the reverted tree against the
// last scenario's baseline and raise a code -> spec change for the revert, and
// a live agent would then spend a model call summarizing a tree that is about
// to be replaced.
func (h *harness) restoreSpec(ctx context.Context, current *spec.SystemContext, start baseline) error {
	restored := *current
	restored.Spec = start.spec
	if restored.Annotations == nil {
		restored.Annotations = map[string]string{}
	}
	restored.SetDefaults()
	hash, err := spec.HashSystemContextSpec(restored.Spec)
	if err != nil {
		return err
	}
	restored.Annotations[specapi.OriginAnnotation] = specapi.OriginIngest
	restored.Annotations[specapi.OriginHashAnnotation] = hash
	if err := applyTyped(ctx, h.client, &restored); err != nil {
		return err
	}
	_, conditions := specsync.Conditions(specsync.Input{
		Name:              current.Name,
		Generation:        restored.GetGeneration(),
		Spec:              restored.Spec,
		Observed:          start.observed,
		SyncedFingerprint: start.observed.Fingerprint,
	}, current.Status.Conditions)
	return h.patchStatus(ctx, current.Name, map[string]any{
		"observedGeneration": restored.GetGeneration(),
		"observedCommit":     start.commit,
		"observed":           start.observed,
		"realizedSpecHash":   hash,
		"realizedSpec":       &restored.Spec,
		"syncedCommit":       start.commit,
		"syncedFingerprint":  start.observed.Fingerprint,
		"syncedObserved":     start.observed,
		"conditions":         conditions,
	})
}

// keyedLists names the spec lists that are keyed, and the key each is keyed by.
// The CRD declares them as list maps, so a scenario patch addresses one entry by
// its key and leaves the others alone, exactly as a server side apply would.
var keyedLists = map[string]string{
	"requirements": "id",
	"interfaces":   "name",
}

// mergeKeyedList merges a scenario's list into the list the spec already holds.
// An entry that says {"$patch": "delete"} takes its key away; any other entry
// merges over the stored one by key, so a scenario changes the fields it names
// and keeps the ones it does not (a stored code ref, for one, is canonical and
// cannot be written by hand). The order the spec already had is kept, and new
// keys go on the end.
func mergeKeyedList(current, patch any, key string) ([]any, error) {
	existing, _ := current.([]any)
	incoming, _ := patch.([]any)
	order := []string{}
	entries := map[string]map[string]any{}
	for _, raw := range existing {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry[key].(string)
		if name == "" {
			continue
		}
		if _, seen := entries[name]; !seen {
			order = append(order, name)
		}
		entries[name] = entry
	}
	for _, raw := range incoming {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("eval: a %s entry is not an object", key)
		}
		name, _ := entry[key].(string)
		if name == "" {
			return nil, fmt.Errorf("eval: a %s entry names no %s", key, key)
		}
		if entry["$patch"] == "delete" {
			delete(entries, name)
			continue
		}
		merged := map[string]any{}
		for field, value := range entries[name] {
			merged[field] = value
		}
		for field, value := range entry {
			if field == "$patch" {
				continue
			}
			merged[field] = value
		}
		if _, seen := entries[name]; !seen {
			order = append(order, name)
		}
		entries[name] = merged
	}
	out := make([]any, 0, len(entries))
	for _, name := range order {
		if entry, ok := entries[name]; ok {
			out = append(out, entry)
		}
	}
	return out, nil
}

// checkPatchRefs refuses a scenario whose code refs are not in the stored form.
// A bare name is what a model may write, because the draft parser canonicalizes
// it; a server side apply stores the ref exactly as written, and a bare one is
// refused later by the validator with an error that names the object and not
// the scenario. Catching it here names the file the mistake is in.
func checkPatchRefs(name string, patch map[string]any) error {
	requirements, _ := patch["requirements"].([]any)
	for _, raw := range requirements {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		refs, _ := entry["codeRefs"].([]any)
		for _, rawRef := range refs {
			ref, _ := rawRef.(string)
			valid := ref == ""
			for _, prefix := range spec.CodeRefPrefixes {
				if strings.HasPrefix(ref, prefix) {
					valid = true
				}
			}
			if !valid {
				return fmt.Errorf("eval: the patch for %s names the code ref %q, which is not one of the prefixes %v", name, ref, spec.CodeRefPrefixes)
			}
		}
	}
	return nil
}

// applyPatch writes one scenario's spec edit the way a person writes one: read
// the spec, change what the scenario names, write the whole thing back. It is
// not a server side apply because a merge by key can add and change an entry
// but cannot take one away, and a removal scenario needs the entry gone. The
// removal is spelled the same way a Kubernetes apply spells one, with a
// {"$patch": "delete"} entry, so a scenario says what it means.
func (h *harness) applyPatch(ctx context.Context, name string, patch map[string]any) error {
	if err := checkPatchRefs(name, patch); err != nil {
		return err
	}
	// The controller writes the status while this runs, so the read and the
	// write can disagree by one resource version. A conflict means somebody
	// else wrote; the read is taken again and the merge reapplied.
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = h.writePatch(ctx, name, patch)
		if err == nil || !apierrors.IsConflict(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return err
}

func (h *harness) writePatch(ctx context.Context, name string, patch map[string]any) error {
	object, err := h.client.Get(ctx, specapi.SystemContextGVR, h.namespace, name)
	if err != nil {
		return err
	}
	current, found, err := unstructured.NestedMap(object.Object, "spec")
	if err != nil {
		return err
	}
	if !found {
		current = map[string]any{}
	}
	for key, value := range patch {
		keyField, keyed := keyedLists[key]
		if !keyed {
			current[key] = value
			continue
		}
		merged, err := mergeKeyedList(current[key], value, keyField)
		if err != nil {
			return err
		}
		current[key] = merged
	}
	if err := unstructured.SetNestedMap(object.Object, current, "spec"); err != nil {
		return err
	}
	_, err = h.client.Apply(ctx, object)
	return err
}

// setAgentKind points the Repository's agent at the kind the spec -> code half
// of one scenario must use. A live run names the live kind; the scripted
// baseline names the scenario file that carries this scenario's edits. A reset
// with no scenario file and no live agent leaves the agent alone, which is what
// a drift scenario wants: its code -> spec pass sets the agent itself.
func (h *harness) setAgentKind(ctx context.Context, scenarioFile string) error {
	kind := ""
	switch {
	case h.options.Agent != "":
		kind = h.options.Agent
	case scenarioFile != "":
		kind = agentfactory.Scripted + ":" + scenarioFile
	}
	if kind == "" {
		return nil
	}
	repository, err := h.typedRepository(ctx)
	if err != nil {
		return err
	}
	repository.Spec.Agent = &spec.AgentSpec{Kind: kind}
	return applyTyped(ctx, h.client, repository)
}

func (h *harness) typedRepository(ctx context.Context) (*spec.Repository, error) {
	object, err := h.client.Get(ctx, specapi.RepositoryGVR, h.namespace, h.repository.Name)
	if err != nil {
		return nil, err
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		return nil, fmt.Errorf("eval: %s is not a Repository", h.repository.Name)
	}
	return repository, nil
}

func (h *harness) repositoryStatus(ctx context.Context) (spec.RepositoryStatus, error) {
	repository, err := h.typedRepository(ctx)
	if err != nil {
		return spec.RepositoryStatus{}, err
	}
	return repository.Status, nil
}

func (h *harness) context(ctx context.Context, name string) (*spec.SystemContext, error) {
	object, err := h.client.Get(ctx, specapi.SystemContextGVR, h.namespace, name)
	if err != nil {
		return nil, err
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return nil, fmt.Errorf("eval: %s is not a SystemContext", name)
	}
	return systemContext, nil
}

func (h *harness) contexts(ctx context.Context) ([]spec.SystemContext, error) {
	listed, err := h.client.List(ctx, specapi.SystemContextGVR, h.namespace)
	if err != nil {
		return nil, err
	}
	out := []spec.SystemContext{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		systemContext, ok := typed.(*spec.SystemContext)
		if !ok || systemContext.Spec.Repository != h.repository.Name {
			continue
		}
		out = append(out, *systemContext)
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out, nil
}

func (h *harness) changes(ctx context.Context, name string) ([]spec.SpecChange, error) {
	listed, err := h.client.List(ctx, specapi.SpecChangeGVR, h.namespace)
	if err != nil {
		return nil, err
	}
	out := []spec.SpecChange{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok {
			continue
		}
		if name == "" || change.Spec.SystemContext == name {
			out = append(out, *change)
		}
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out, nil
}

func (h *harness) changeCount(ctx context.Context, name string) int {
	changes, err := h.changes(ctx, name)
	if err != nil {
		return 0
	}
	count := 0
	for _, change := range changes {
		if change.Spec.Direction == specapi.DirectionSpecToCode {
			count++
		}
	}
	return count
}

func (h *harness) deleteChanges(ctx context.Context, name string) error {
	changes, err := h.changes(ctx, name)
	if err != nil {
		return err
	}
	for _, change := range changes {
		if err := h.client.Delete(ctx, specapi.SpecChangeGVR, h.namespace, change.Name); err != nil {
			return err
		}
	}
	return nil
}

func (h *harness) patchStatus(ctx context.Context, name string, status map[string]any) error {
	_, err := h.client.PatchStatus(ctx, specapi.SystemContextGVR, h.namespace, name, status)
	return err
}

// waitPopulated waits for the populate step to settle, and returns as soon as
// the Repository says it cannot be indexed at all: a manifest the validator
// would refuse never reaches a phase, and waiting for one would hang a run.
// failedSummaries names the contexts of this fixture whose code -> spec change
// failed, and the message it failed with. It is how a partly populated codebase
// says which part it could not read.
func (h *harness) failedSummaries(ctx context.Context) []string {
	contexts, err := h.contexts(ctx)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, entry := range contexts {
		if entry.Spec.Intent != "" {
			continue
		}
		changes, err := h.changes(ctx, entry.Name)
		if err != nil {
			continue
		}
		for _, change := range changes {
			if change.Spec.Direction != specapi.DirectionCodeToSpec {
				continue
			}
			message := strings.TrimSpace(change.Status.Message)
			if message == "" {
				// A change that failed before it could be described still has
				// the model's own output, which is the only clue left.
				message = "no message; the agent answered: " + strings.TrimSpace(change.Status.AgentLog)
			}
			out = append(out, entry.Name+": "+firstLine(limitText(message, 400)))
			break
		}
	}
	sort.Strings(out)
	return out
}

func (h *harness) waitPopulated(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, h.options.Timeout)
	defer cancel()
	return h.wait(bounded, "the Repository to reach Populated", func() bool {
		status, err := h.repositoryStatus(ctx)
		if err != nil {
			return false
		}
		if status.Phase == specapi.PhasePopulated || status.Phase == specapi.PhaseFailed {
			return true
		}
		return blocked(status)
	})
}

// blocked reports the conditions that say the Repository will never populate.
func blocked(status spec.RepositoryStatus) bool {
	for _, condition := range status.Conditions {
		if condition.Status == metav1.ConditionFalse && condition.Reason == specapi.ReasonSourceInvalid {
			return true
		}
	}
	return false
}

func (h *harness) waitChange(ctx context.Context, name, direction string) (spec.SpecChange, error) {
	var found spec.SpecChange
	err := h.wait(ctx, "the "+direction+" change of "+name, func() bool {
		changes, err := h.changes(ctx, name)
		if err != nil {
			return false
		}
		for _, change := range changes {
			if change.Spec.Direction != direction {
				continue
			}
			if change.Status.Phase == specapi.PhaseSucceeded || change.Status.Phase == specapi.PhaseFailed {
				found = change
				return true
			}
		}
		return false
	})
	return found, err
}

// waitInterfaces waits for the re-ingest that follows a realize to observe the
// interfaces the scenario declared, and reports the ones that never appeared or
// that should have gone and did not. A removal is graded here: a spec that lost
// an interface and code that still exports it is a change that did not happen.
func (h *harness) waitInterfaces(ctx context.Context, name string, expected, removed []string) []string {
	if len(expected) == 0 && len(removed) == 0 {
		return nil
	}
	wrong := []string{}
	deadline := time.Now().Add(30 * time.Second)
	for {
		wrong = wrong[:0]
		current, err := h.context(ctx, name)
		if err == nil {
			observed := map[string]bool{}
			for _, entry := range current.Status.Observed.Interfaces {
				observed[entry.Name] = true
			}
			for _, want := range expected {
				if !observed[want] {
					wrong = append(wrong, "missing "+want)
				}
			}
			for _, gone := range removed {
				if observed[gone] {
					wrong = append(wrong, "still observed "+gone)
				}
			}
			if len(wrong) == 0 {
				return nil
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return wrong
		}
		time.Sleep(pollInterval)
	}
}

// runAcceptance copies the scenario's hidden tests into the tree, runs the
// fixture's acceptance command, and takes them out again. The tree at this
// point is the one the agent committed, so the tests run against the code the
// change produced and nothing else.
func (h *harness) runAcceptance(ctx context.Context, command []string, files []AcceptanceFile) (bool, error) {
	written := []string{}
	defer func() {
		for _, path := range written {
			_ = os.Remove(path)
		}
	}()
	for _, file := range files {
		target := filepath.Join(h.dir, filepath.FromSlash(file.Path))
		if !strings.HasPrefix(target, h.dir+string(os.PathSeparator)) {
			return false, fmt.Errorf("eval: acceptance file %q is outside the tree", file.Path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(target, []byte(file.Contents), 0o644); err != nil {
			return false, err
		}
		written = append(written, target)
	}
	if len(command) == 0 {
		return true, nil
	}
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Dir = h.dir
	output, err := process.CombinedOutput()
	if err == nil {
		return true, nil
	}
	return false, fmt.Errorf("the acceptance tests failed: %v: %s", err, tail(string(output)))
}

func (h *harness) wait(ctx context.Context, what string, predicate func() bool) error {
	for {
		if predicate() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("eval: timed out waiting for %s", what)
		case <-time.After(pollInterval):
		}
	}
}

func (h *harness) git(ctx context.Context, args ...string) error {
	return runGit(ctx, h.dir, args...)
}

func (h *harness) deleteBranches(ctx context.Context) error {
	command := exec.CommandContext(ctx, "git", "-C", h.dir, "branch", "--list", "spec/*", "--format=%(refname:short)")
	output, err := command.Output()
	if err != nil {
		return nil
	}
	for branch := range strings.FieldsSeq(string(output)) {
		if err := h.git(ctx, "branch", "-D", branch); err != nil {
			return err
		}
	}
	return nil
}

type defaults interface {
	SetDefaults()
}

func applyTyped(ctx context.Context, client *kcpclient.Client, value any) error {
	if setter, ok := value.(defaults); ok {
		setter.SetDefaults()
	}
	// The controller would report a manifest the validator refuses as a
	// condition nobody reads, and a run would wait for a phase that never
	// comes. The harness refuses it where the mistake was made instead.
	if result := spec.ValidateAny(value); !result.OK() {
		return fmt.Errorf("eval: the manifest the harness built is invalid: %w", result.Err())
	}
	object, err := kcpclient.Unstructured(value)
	if err != nil {
		return err
	}
	_, err = client.Apply(ctx, object)
	return err
}

// populateError says why a Repository did not populate: the phase it stopped
// at and the conditions that stopped it.
func populateError(status spec.RepositoryStatus) string {
	message := status.Phase
	for _, condition := range status.Conditions {
		if condition.Status == metav1.ConditionFalse {
			message += "; " + condition.Type + ": " + condition.Reason + ": " + condition.Message
		}
	}
	return strings.TrimSpace(message)
}

// forget removes the objects of one fixture's previous run, so a re-run does
// not grade itself against its own history, and it waits until they are really
// gone.
//
// A change is deleted when its context belongs to this repository or when the
// context it names does not exist at all: a run that is stopped part way can
// leave a change behind whose context is already deleted, and a context
// recreated with the same name would then be read against the old one's
// baseline and report a drift nobody caused.
func forget(ctx context.Context, client *kcpclient.Client, namespace, repository string) error {
	owned := map[string]bool{}
	live := map[string]bool{}
	contexts, err := client.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return err
	}
	for index := range contexts.Items {
		object := &contexts.Items[index]
		live[object.GetName()] = true
		owner, _, _ := unstructured.NestedString(object.Object, "spec", "repository")
		if owner == repository {
			owned[object.GetName()] = true
		}
	}
	changes, err := client.List(ctx, specapi.SpecChangeGVR, namespace)
	if err != nil {
		return err
	}
	for index := range changes.Items {
		object := &changes.Items[index]
		owner, _, _ := unstructured.NestedString(object.Object, "spec", "systemContext")
		if owned[owner] || !live[owner] {
			if err := client.Delete(ctx, specapi.SpecChangeGVR, namespace, object.GetName()); err != nil {
				return err
			}
		}
	}
	for name := range owned {
		if err := client.Delete(ctx, specapi.SystemContextGVR, namespace, name); err != nil {
			return err
		}
	}
	if err := client.Delete(ctx, specapi.RepositoryGVR, namespace, repository); err != nil {
		return err
	}
	return waitGone(ctx, client, namespace, owned, repository)
}

// waitGone waits until every object forget removed is no longer listed. An
// ingest that raced a delete would otherwise adopt the object that was on its
// way out and keep the baseline of the tree before it.
func waitGone(ctx context.Context, client *kcpclient.Client, namespace string, contexts map[string]bool, repository string) error {
	for {
		remaining := false
		listed, err := client.List(ctx, specapi.SystemContextGVR, namespace)
		if err != nil {
			return err
		}
		for index := range listed.Items {
			if contexts[listed.Items[index].GetName()] {
				remaining = true
			}
		}
		changes, err := client.List(ctx, specapi.SpecChangeGVR, namespace)
		if err != nil {
			return err
		}
		for index := range changes.Items {
			owner, _, _ := unstructured.NestedString(changes.Items[index].Object, "spec", "systemContext")
			if contexts[owner] {
				remaining = true
			}
		}
		repositories, err := client.List(ctx, specapi.RepositoryGVR, namespace)
		if err != nil {
			return err
		}
		for index := range repositories.Items {
			if repositories.Items[index].GetName() == repository {
				remaining = true
			}
		}
		if !remaining {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("eval: timed out waiting for the objects of %s to be deleted", repository)
		case <-time.After(pollInterval):
		}
	}
}

func initRepository(ctx context.Context, dir string) (string, error) {
	if err := runGit(ctx, dir, "init", "-q", "-b", "main"); err != nil {
		return "", err
	}
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	if err := runGit(ctx, dir, "-c", "user.email="+gitAuthorMail, "-c", "user.name="+gitAuthorName, "commit", "-qm", "fixture"); err != nil {
		return "", err
	}
	return headOf(ctx, dir), nil
}

func headOf(ctx context.Context, dir string) string {
	command := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--verify", "HEAD")
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func runGit(ctx context.Context, dir string, args ...string) error {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("eval: git %s: %w: %s", strings.Join(args, " "), err, tail(string(output)))
	}
	return nil
}

// writeScenarioFiles writes one merged scripted scenario per scenario: the
// fixture's code -> spec drafts plus that scenario's realize steps. The file
// lives outside the working tree, so the agent under test cannot read the
// intended edit out of the tree it is editing.
func writeScenarioFiles(workDir string, fixture Fixture) (map[string]string, error) {
	dir := filepath.Join(workDir, "eval-scenarios")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, scenario := range fixture.Scenarios {
		realize := map[string][]scriptedagent.Step{}
		for _, target := range scenario.resolvedTargets() {
			if len(target.Realize) > 0 {
				realize[target.Context] = target.Realize
			}
		}
		merged := scriptedagent.Scenario{
			Contexts: fixture.Drafts.Contexts,
			Realize:  realize,
		}
		encoded, err := yaml.Marshal(merged)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, fixture.Name+"-"+scenario.Name+".yaml")
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return nil, err
		}
		files[scenario.Name] = path
	}
	return files, nil
}

func interfaceNames(declared []spec.Interface) []string {
	out := make([]string, 0, len(declared))
	for _, entry := range declared {
		out = append(out, entry.Name)
	}
	return out
}

func orScripted(kind string) string {
	if kind == "" {
		return "scripted"
	}
	return kind
}

// firstLine keeps one line of a failure, and limitText bounds it, so a report
// stays readable when a model answered with a whole JSON document.
func firstLine(value string) string {
	line, _, _ := strings.Cut(value, "\n")
	return line
}

func limitText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum] + "..."
}

func tail(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= 2000 {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-2000:]
}
