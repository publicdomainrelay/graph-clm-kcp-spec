package evalrun

import (
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"

	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
)

// modelRunner is the raw prompt entry point of a host inside the model. The
// Claude CLI agent and the pi agent both have it, so a run can ask the model to
// do something the summarize and realize contracts do not describe: change the
// specification by editing the context document.
type modelRunner interface {
	Run(ctx context.Context, prompt string, extra map[string]string) (string, string, error)
}

// driveClm is the CLM path driven, not simulated. The harness writes the
// context document the way the host renders it, asks the model in natural
// language to change the spec by editing that document, and lets the host
// inside the model apply the delta with `specctl clm apply`. Nothing here edits
// the specification: if the model does not change the document, no spec change
// is raised and the scenario fails for that reason.
func (h *harness) driveClm(ctx context.Context, scenario Scenario) error {
	built, err := h.agent(h.realizeKind)
	if err != nil {
		return err
	}
	runner, ok := built.(modelRunner)
	if !ok {
		return fmt.Errorf("eval: the %s agent cannot be driven with a prompt", h.realizeKind)
	}
	document, err := h.renderContextDoc(ctx, scenario.Context)
	if err != nil {
		return err
	}
	_, stderr, err := runner.Run(ctx, ClmPrompt(scenario.Context, document, scenario.Request), map[string]string{
		"SPECD_CLM_CONTEXT": scenario.Context,
		"SPECD_CLM_REPO":    h.dir,
		"SPECD_CLM_ROOT":    h.dir,
	})
	if err != nil {
		return fmt.Errorf("eval: the model did not answer for %s: %w: %s", scenario.Context, err, firstLine(stderr))
	}
	return nil
}

// ClmPrompt is the ask of a CLM scenario: the natural language request a person
// would make, and the document the model has to change to grant it.
func ClmPrompt(contextName, document, request string) string {
	builder := &strings.Builder{}
	builder.WriteString("You change the specification of one context of a codebase.\n\n")
	fmt.Fprintf(builder, "The specification of the context %s is the context document at:\n\n  %s\n\n", contextName, document)
	builder.WriteString("Read that document, then edit it so it says the following:\n\n")
	builder.WriteString(strings.TrimSpace(request))
	builder.WriteString("\n\n")
	builder.WriteString("Edit that document only. Do not edit code and do not run git. ")
	builder.WriteString("The host applies your edit to the specification when the turn ends.\n")
	return builder.String()
}

// renderContextDoc writes the context document the way the host renders it, so
// the model always has something to edit and the document is the one the host
// would have produced. specctl is the only renderer, so it is the one used.
func (h *harness) renderContextDoc(ctx context.Context, name string) (string, error) {
	args := []string{"clm", "render", "--context", name}
	if h.options.Kubeconfig != "" {
		args = append(args, "--kubeconfig", h.options.Kubeconfig)
	}
	if h.options.Workspace != "" {
		args = append(args, "--workspace", h.options.Workspace)
	}
	if h.namespace != "" {
		args = append(args, "--namespace", h.namespace)
	}
	command := exec.CommandContext(ctx, specctlPath(), args...)
	command.Dir = h.dir
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("eval: render the context document of %s: %w: %s", name, err, firstLine(string(output)))
	}
	repositoryName := ""
	if h.repository != nil {
		repositoryName = h.repository.Name
	}
	path := agent.ContextDocPath(statedir.ClmDocDir(), repositoryName, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, output, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
