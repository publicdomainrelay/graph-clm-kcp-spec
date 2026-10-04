package stripbodies

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Result struct {
	Files int `json:"files"`

	Stripped int `json:"stripped"`

	Skipped []string `json:"skipped,omitempty"`
}

func Strip(dir string, files []string) (Result, error) {
	result := Result{}
	for _, file := range files {
		path := filepath.Join(dir, filepath.FromSlash(file))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		if isTestFile(file) || !needsStrip(file) {
			if !isTestFile(file) {
				result.Skipped = append(result.Skipped, file)
			}
			continue
		}
		result.Files++
		var stripped int
		var err error
		if strings.HasSuffix(file, ".go") {
			stripped, err = stripGo(path)
		} else {
			stripped, err = stripTS(path)
		}
		if err != nil {
			return result, fmt.Errorf("stripbodies: %s: %w", file, err)
		}
		result.Stripped += stripped
	}
	sort.Strings(result.Skipped)
	return result, nil
}

func isTestFile(file string) bool {
	return strings.HasSuffix(file, "_test.go") || strings.HasSuffix(file, "_test.ts")
}
