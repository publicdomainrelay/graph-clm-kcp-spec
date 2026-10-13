package agentrun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

var start = time.Date(2026, 10, 12, 9, 8, 7, 0, time.UTC)

type stubAgent struct {
	draft agent.SpecDraft

	result agent.RealizeResult

	err error

	calls int
}

func (s *stubAgent) Summarize(context.Context, agent.ContextBundle) (agent.SpecDraft, error) {
	s.calls++
	return s.draft, s.err
}

func (s *stubAgent) Realize(context.Context, agent.RealizeRequest) (agent.RealizeResult, error) {
	s.calls++
	return s.result, s.err
}

type fakeCluster struct {
	objects map[string]*unstructured.Unstructured

	refusesCreate bool
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: map[string]*unstructured.Unstructured{}}
}

func (f *fakeCluster) Create(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if f.refusesCreate {
		return nil, errors.New("the cluster refused the record")
	}
	stored := object.DeepCopy()
	f.objects[object.GetName()] = stored
	return stored.DeepCopy(), nil
}

func (f *fakeCluster) PatchStatus(_ context.Context, _ schema.GroupVersionResource, _, name string, status map[string]any) (*unstructured.Unstructured, error) {
	stored, ok := f.objects[name]
	if !ok {
		return nil, errors.New("no such agent run: " + name)
	}
	current, _, err := unstructured.NestedMap(stored.Object, "status")
	if err != nil {
		return nil, err
	}
	if current == nil {
		current = map[string]any{}
	}
	for key, value := range status {
		current[key] = value
	}
	if err := unstructured.SetNestedMap(stored.Object, current, "status"); err != nil {
		return nil, err
	}
	return stored.DeepCopy(), nil
}

func (f *fakeCluster) names() []string {
	out := make([]string, 0, len(f.objects))
	for name := range f.objects {
		out = append(out, name)
	}
	return out
}

// clock advances by one step per call, so the start and the end of a run differ.
func clock(step time.Duration) func() time.Time {
	calls := 0
	return func() time.Time {
		value := start.Add(time.Duration(calls) * step)
		calls++
		return value
	}
}

func readRun(t *testing.T, cluster *fakeCluster, name string) *spec.AgentRun {
	t.Helper()
	stored, ok := cluster.objects[name]
	if !ok {
		t.Fatalf("no agent run %q, the cluster holds %v", name, cluster.names())
	}
	typed, err := kcpclient.Typed(stored)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := typed.(*spec.AgentRun)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	return record
}

func TestAgentRunSummarizeRecordsASucceededRun(t *testing.T) {
	cluster := newFakeCluster()
	inner := &stubAgent{draft: agent.SpecDraft{Summary: "the calc package adds"}}
	runs := []string{}
	wrapped := Wrap(inner, Options{
		Cluster: cluster,
		Change:  "calc-7",
		Context: "calc",
		Agent:   "scripted",
		Command: "claude -p",
		Now:     clock(5 * time.Second),
		OnRun:   func(name string) { runs = append(runs, name) },
	})

	draft, err := wrapped.Summarize(context.Background(), agent.ContextBundle{})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Summary != "the calc package adds" || inner.calls != 1 {
		t.Fatalf("draft = %+v after %d call(s), want the inner agent's", draft, inner.calls)
	}

	if names := cluster.names(); len(names) != 1 {
		t.Fatalf("runs = %v, want the one the call wrote", names)
	}
	name := "calc-7-summarize-20261012t090807"
	record := readRun(t, cluster, name)
	if record.Spec.Purpose != Summarize || record.Spec.Change != "calc-7" {
		t.Errorf("spec = %+v, want the summarize of calc-7", record.Spec)
	}
	if record.Spec.Context != "calc" || record.Spec.Agent != "scripted" || record.Spec.Command != "claude -p" {
		t.Errorf("spec = %+v, want the context, the agent kind and its command", record.Spec)
	}
	if record.Status.Phase != specapi.PhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded: %s", record.Status.Phase, record.Status.Message)
	}
	if record.Status.StartedAt != start.Format(time.RFC3339) {
		t.Errorf("startedAt = %q, want %q", record.Status.StartedAt, start.Format(time.RFC3339))
	}
	if record.Status.FinishedAt != start.Add(5*time.Second).Format(time.RFC3339) {
		t.Errorf("finishedAt = %q, want the end of the call", record.Status.FinishedAt)
	}
	if record.Status.DurationSeconds != 5 {
		t.Errorf("durationSeconds = %v, want 5", record.Status.DurationSeconds)
	}
	if record.Status.Message != Ok {
		t.Errorf("message = %q, want %q", record.Status.Message, Ok)
	}
	if len(runs) != 1 || runs[0] != name {
		t.Errorf("OnRun got %v, want %q", runs, name)
	}
}

func TestAgentRunRealizeRecordsAFailedRun(t *testing.T) {
	cluster := newFakeCluster()
	inner := &stubAgent{err: errors.New(strings.Repeat("boom ", 300))}
	wrapped := Wrap(inner, Options{
		Cluster: cluster,
		Change:  "calc-9",
		Context: "calc",
		Now:     clock(2 * time.Second),
	})

	result, err := wrapped.Realize(context.Background(), agent.RealizeRequest{})
	if err == nil {
		t.Fatal("the inner agent's error was swallowed")
	}
	if result.Summary != "" {
		t.Errorf("result = %+v, want the inner agent's", result)
	}

	record := readRun(t, cluster, "calc-9-realize-20261012t090807")
	if record.Spec.Purpose != Realize || record.Spec.Change != "calc-9" {
		t.Errorf("spec = %+v, want the realize of calc-9", record.Spec)
	}
	if record.Status.Phase != specapi.PhaseFailed {
		t.Errorf("phase = %q, want Failed", record.Status.Phase)
	}
	if record.Status.FinishedAt == "" {
		t.Error("finishedAt is empty")
	}
	if record.Status.DurationSeconds != 2 {
		t.Errorf("durationSeconds = %v, want 2", record.Status.DurationSeconds)
	}
	if !strings.HasPrefix(record.Status.Message, "boom boom boom ") {
		t.Errorf("message = %q, want the agent's error text", record.Status.Message)
	}
	if cut := len([]rune(record.Status.Message)); cut != messageLimit {
		t.Errorf("message holds %d character(s), want the error text cut to %d", cut, messageLimit)
	}
}

func TestAgentRunOnRunGetsTheNameOfTheRecord(t *testing.T) {
	cluster := newFakeCluster()
	runs := []string{}
	wrapped := Wrap(&stubAgent{}, Options{
		Cluster: cluster,
		Change:  "calc-11",
		Now:     clock(time.Second),
		OnRun:   func(name string) { runs = append(runs, name) },
	})

	if _, err := wrapped.Summarize(context.Background(), agent.ContextBundle{}); err != nil {
		t.Fatal(err)
	}

	if len(runs) != 1 {
		t.Fatalf("OnRun got %v, want one name", runs)
	}
	if _, held := cluster.objects[runs[0]]; !held {
		t.Errorf("OnRun got %q, which names no record", runs[0])
	}
}

func TestAgentRunClusterFailureKeepsTheResult(t *testing.T) {
	cluster := newFakeCluster()
	cluster.refusesCreate = true
	inner := &stubAgent{draft: agent.SpecDraft{Summary: "the calc package adds"}}
	wrapped := Wrap(inner, Options{
		Cluster: cluster,
		Change:  "calc-13",
		Now:     clock(time.Second),
	})

	draft, err := wrapped.Summarize(context.Background(), agent.ContextBundle{})
	if err != nil {
		t.Fatalf("a cluster that refused the record changed the result: %v", err)
	}
	if draft.Summary != inner.draft.Summary {
		t.Errorf("draft = %+v, want the inner agent's", draft)
	}
	if names := cluster.names(); len(names) != 0 {
		t.Errorf("runs = %v, want none", names)
	}
}
