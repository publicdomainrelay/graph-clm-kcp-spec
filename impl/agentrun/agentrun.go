// Package agentrun decorates an agent.Agent so that every call it makes is an
// AgentRun object: the record is created before the agent runs and its status
// says how the call ended. Recording is best effort, so a cluster that refuses
// the create or the status patch costs a log line and never the agent's own
// result.
package agentrun

import (
	"context"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// Summarize and Realize are the purposes a run records: the two calls an
// agent.Agent offers.
const (
	Summarize = "summarize"

	Realize = "realize"
)

// Ok is the message of a run that ended without an error.
const Ok = "ok"

const (
	nameLayout = "20060102t150405"

	messageLimit = 1000
)

// Cluster is the part of the object client a run needs: create the record, then
// patch its status.
type Cluster interface {
	Create(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

// Options is what the wrapper knows about the run it records: the change and
// the context the call belongs to, the agent kind and the command behind it,
// and the hooks the caller wants. A nil Now is time.Now and a nil Log discards.
type Options struct {
	Cluster Cluster

	Namespace string

	Change string

	Context string

	Agent string

	Command string

	Now func() time.Time

	OnRun func(name string)

	Log *slog.Logger
}

// Wrap returns the agent that records an AgentRun around every call it passes
// on. The inner agent's result is the caller's result either way.
func Wrap(inner agent.Agent, options Options) agent.Agent {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Log == nil {
		options.Log = slog.New(slog.DiscardHandler)
	}
	return &run{inner: inner, options: options}
}

type run struct {
	inner agent.Agent

	options Options
}

func (r *run) Summarize(ctx context.Context, bundle agent.ContextBundle) (agent.SpecDraft, error) {
	name, started := r.begin(ctx, Summarize)
	draft, err := r.inner.Summarize(ctx, bundle)
	r.end(ctx, name, started, err)
	return draft, err
}

func (r *run) Realize(ctx context.Context, request agent.RealizeRequest) (agent.RealizeResult, error) {
	name, started := r.begin(ctx, Realize)
	result, err := r.inner.Realize(ctx, request)
	r.end(ctx, name, started, err)
	return result, err
}

// begin records the run before the agent is called: the object says what the
// call is, and its status says it is running. It returns the name whatever
// happens, so the status write after the call addresses the same object.
func (r *run) begin(ctx context.Context, purpose string) (string, time.Time) {
	started := r.options.Now().UTC()
	name := Name(r.options.Change, purpose, started)
	record := &spec.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.options.Namespace},
		Spec: spec.AgentRunSpec{
			Purpose: purpose,
			Change:  r.options.Change,
			Context: r.options.Context,
			Agent:   r.options.Agent,
			Command: r.options.Command,
		},
	}
	record.SetDefaults()
	object, err := kcpclient.Unstructured(record)
	if err != nil {
		r.options.Log.Warn("could not build the agent run", "run", name, "err", err)
		return name, started
	}
	if r.options.Cluster == nil {
		return name, started
	}
	if _, err := r.options.Cluster.Create(ctx, object); err != nil {
		r.options.Log.Warn("could not create the agent run", "run", name, "err", err)
		return name, started
	}
	status := map[string]any{
		"phase":     specapi.PhaseRunning,
		"startedAt": started.Format(time.RFC3339),
	}
	if _, err := r.options.Cluster.PatchStatus(ctx, specapi.AgentRunGVR, r.options.Namespace, name, status); err != nil {
		r.options.Log.Warn("could not mark the agent run running", "run", name, "err", err)
	}
	return name, started
}

// end says how the call ended and hands the name to the caller's OnRun hook.
func (r *run) end(ctx context.Context, name string, started time.Time, failure error) {
	finished := r.options.Now().UTC()
	phase := specapi.PhaseSucceeded
	message := Ok
	if failure != nil {
		phase = specapi.PhaseFailed
		message = cut(failure.Error(), messageLimit)
	}
	if r.options.Cluster != nil {
		status := map[string]any{
			"phase":           phase,
			"finishedAt":      finished.Format(time.RFC3339),
			"durationSeconds": finished.Sub(started).Seconds(),
			"message":         message,
		}
		if _, err := r.options.Cluster.PatchStatus(ctx, specapi.AgentRunGVR, r.options.Namespace, name, status); err != nil {
			r.options.Log.Warn("could not finish the agent run", "run", name, "err", err)
		}
	}
	if r.options.OnRun != nil {
		r.options.OnRun(name)
	}
}

// Name is the object name of a run: the change, the purpose and the start time,
// which keeps two calls of the same change apart.
func Name(change, purpose string, started time.Time) string {
	return change + "-" + purpose + "-" + started.UTC().Format(nameLayout)
}

func cut(message string, limit int) string {
	runes := []rune(message)
	if len(runes) <= limit {
		return message
	}
	return string(runes[:limit])
}
