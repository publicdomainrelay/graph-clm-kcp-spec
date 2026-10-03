// Package codegraphcli runs the codegraph command for its two context
// commands. The sqlite reader in codegraphsqlite answers "what does the index
// say"; this package answers "show me the code", which is what a model needs
// and only the CLI renders.
package codegraphcli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const DefaultTool = "codegraph"

type Runner struct {
	Tool string

	Dir string
}

func (r Runner) tool() string {
	if r.Tool == "" {
		return DefaultTool
	}
	return r.Tool
}

// Context builds the codegraph context for a task. The output is markdown with
// code blocks, so it is the largest thing in a bundle and the budget cuts it
// first.
func (r Runner) Context(ctx context.Context, task string, maxNodes int) (string, error) {
	args := []string{"context", "-p", r.Dir, "-f", "markdown"}
	if maxNodes > 0 {
		args = append(args, "-n", fmt.Sprint(maxNodes))
	}
	args = append(args, strings.Fields(task)...)
	return r.run(ctx, args...)
}

// Node reads one symbol's source and its caller trail. It is the precise read
// behind a single code reference, so a bundle asks for a few of these and not
// for the whole index.
func (r Runner) Node(ctx context.Context, name string) (string, error) {
	return r.run(ctx, "node", "-p", r.Dir, name)
}

func (r Runner) run(ctx context.Context, args ...string) (string, error) {
	if _, err := exec.LookPath(r.tool()); err != nil {
		return "", fmt.Errorf("codegraphcli: %s is not on PATH: %w", r.tool(), err)
	}
	command := exec.CommandContext(ctx, r.tool(), args...)
	command.Dir = r.Dir
	output, err := command.Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", fmt.Errorf("codegraphcli: %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", fmt.Errorf("codegraphcli: %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}
