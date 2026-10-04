package bundle

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
)

func ReadContextDoc(docDir, repository, context string) (string, error) {
	path := agent.ContextDocPath(docDir, repository, context)
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

func WriteContextDoc(docDir, repository, context, modelZone string, refs []agent.ResolvedRef, managedBudget int) (string, error) {
	if modelZone == "" {
		existing, err := ReadContextDoc(docDir, repository, context)
		if err != nil {
			return "", err
		}
		modelZone = existing
	}
	path := agent.ContextDocPath(docDir, repository, context)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("bundle: create %s: %w", filepath.Dir(path), err)
	}
	document := agent.ComposeContextDoc(modelZone, refs, managedBudget)
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		return "", fmt.Errorf("bundle: write %s: %w", path, err)
	}
	return path, nil
}
