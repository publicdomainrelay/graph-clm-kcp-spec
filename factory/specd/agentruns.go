package specd

import (
	"context"
	"fmt"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// recordAgentRuns appends a run to the status of every change the run belongs
// to: the change that named it, and the members of a realize batch that went
// with its leader. The record is best effort, so a change that cannot be
// written costs a log line and never the change itself.
func (c *Controller) recordAgentRuns(ctx context.Context, namespace, run string, changes ...string) {
	for _, change := range changes {
		if err := c.appendAgentRun(ctx, namespace, change, run); err != nil {
			c.log.Warn("could not record the agent run on the change",
				"change", change, "run", run, "err", err)
		}
	}
}

func (c *Controller) appendAgentRun(ctx context.Context, namespace, change, run string) error {
	object, err := c.client.Get(ctx, specapi.SpecChangeGVR, namespace, change)
	if err != nil {
		return err
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return err
	}
	parsed, ok := typed.(*spec.SpecChange)
	if !ok {
		return fmt.Errorf("specd: %s is not a SpecChange", change)
	}
	runs := make([]string, 0, len(parsed.Status.AgentRuns)+1)
	runs = append(runs, parsed.Status.AgentRuns...)
	runs = append(runs, run)
	_, err = c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, change, map[string]any{"agentRuns": runs})
	return err
}

// agentCommand is the command line behind an agent run: the repository's own
// agent when it names one, the controller's otherwise.
func (c *Controller) agentCommand(repository *spec.Repository) string {
	command := c.opts.AgentCommand
	args := c.opts.AgentArgs
	if repository != nil && repository.Spec.Agent != nil {
		if repository.Spec.Agent.Command != "" {
			command = repository.Spec.Agent.Command
		}
		if len(repository.Spec.Agent.Args) > 0 {
			args = repository.Spec.Agent.Args
		}
	}
	return strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
}
