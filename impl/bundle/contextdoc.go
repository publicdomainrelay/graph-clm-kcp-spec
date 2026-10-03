package bundle

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
)

// ReadContextDoc returns the model zone of a context document. A missing
// document is not an error: the first summarize of a context starts from an
// empty model zone.
func ReadContextDoc(repoPath, context string) (string, error) {
	path := agent.ContextDocPath(repoPath, context)
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("bundle: read %s: %w", path, err)
	}
	model, _ := agent.SplitContextDoc(string(contents))
	return model, nil
}

// WriteContextDoc puts the model zone above a managed zone regenerated from the
// observed facts. An empty model zone keeps what the document already holds, so
// a summarize that produced no prose does not erase the last summary.
func WriteContextDoc(repoPath, context, modelZone string, refs []agent.ResolvedRef, managedBudget int) (string, error) {
	if modelZone == "" {
		existing, err := ReadContextDoc(repoPath, context)
		if err != nil {
			return "", err
		}
		modelZone = existing
	}
	path := agent.ContextDocPath(repoPath, context)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("bundle: create %s: %w", filepath.Dir(path), err)
	}
	document := agent.ComposeContextDoc(modelZone, refs, managedBudget)
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		return "", fmt.Errorf("bundle: write %s: %w", path, err)
	}
	return path, nil
}
