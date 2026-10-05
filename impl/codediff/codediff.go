package codediff

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

const (
	addedLine   = "+"
	removedLine = "-"
	noNewline   = "\\"
)

func Build(ctx context.Context, repo, base, head string) (policy.CodeDiff, error) {
	if strings.TrimSpace(base) == "" {
		return policy.CodeDiff{}, fmt.Errorf("codediff: a base ref is required")
	}
	if strings.TrimSpace(head) == "" {
		head = "HEAD"
	}
	baseCommit, err := revParse(ctx, repo, base)
	if err != nil {
		return policy.CodeDiff{}, err
	}
	headCommit, err := revParse(ctx, repo, head)
	if err != nil {
		return policy.CodeDiff{}, err
	}
	patch, err := run(ctx, repo, "diff", "--unified=0", "--no-color", "--no-ext-diff", "--no-renames", "--no-prefix", baseCommit, headCommit)
	if err != nil {
		return policy.CodeDiff{}, err
	}
	diff := policy.CodeDiff{
		APIVersion: policy.APIVersion,
		Kind:       policy.CodeDiffKind,
		Metadata:   policy.ObjectMeta{Name: repositoryName(repo), Namespace: "default"},
		Spec:       policy.CodeDiffSpec{Base: baseCommit, Head: headCommit},
	}
	diff.Spec.Files = parse(patch)
	diff.Sort()
	return diff, nil
}

func revParse(ctx context.Context, repo, ref string) (string, error) {
	out, err := run(ctx, repo, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("codediff: git %s: %s", strings.Join(args, " "), message)
	}
	return out, nil
}

func parse(patch []byte) []policy.CodeDiffFile {
	files := []policy.CodeDiffFile{}
	current := (*policy.CodeDiffFile)(nil)
	basePath := ""
	oldLine, newLine := 0, 0
	scanner := bufio.NewScanner(bytes.NewReader(patch))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if current != nil {
				files = append(files, *current)
			}
			current = &policy.CodeDiffFile{Status: "modified"}
			basePath = ""
			oldLine, newLine = 0, 0
		case current == nil:
			continue
		case strings.HasPrefix(line, "--- "):
			value := headerPath(strings.TrimPrefix(line, "--- "))
			if value == "/dev/null" {
				current.Status = "added"
			} else {
				basePath = value
			}
		case strings.HasPrefix(line, "+++ "):
			value := headerPath(strings.TrimPrefix(line, "+++ "))
			if value == "/dev/null" {
				current.Status = "deleted"
				current.Path = basePath
			} else {
				current.Path = value
			}
		case strings.HasPrefix(line, "@@"):
			oldLine, newLine = hunkHeader(line)
		case strings.HasPrefix(line, noNewline):
		case strings.HasPrefix(line, addedLine):
			current.Added = append(current.Added, policy.CodeDiffLine{
				Line: newLine,
				Text: strings.TrimPrefix(line, addedLine),
			})
			newLine++
		case strings.HasPrefix(line, removedLine):
			current.Removed = append(current.Removed, policy.CodeDiffLine{
				Line: oldLine,
				Text: strings.TrimPrefix(line, removedLine),
			})
			oldLine++
		}
	}
	if current != nil {
		files = append(files, *current)
	}
	return files
}

func headerPath(value string) string {
	trimmed, _, _ := strings.Cut(value, "\t")
	return trimmed
}

func hunkHeader(line string) (int, int) {
	oldStart, newStart := 0, 0
	for field := range strings.FieldsSeq(line) {
		switch {
		case strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "---"):
			oldStart = startOf(strings.TrimPrefix(field, "-"))
		case strings.HasPrefix(field, "+") && !strings.HasPrefix(field, "+++"):
			newStart = startOf(strings.TrimPrefix(field, "+"))
		}
	}
	return oldStart, newStart
}

func startOf(rangeSpec string) int {
	value := rangeSpec
	if index := strings.Index(value, ","); index >= 0 {
		value = value[:index]
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func repositoryName(repo string) string {
	absolute, err := filepath.Abs(repo)
	if err != nil {
		return filepath.Base(repo)
	}
	return filepath.Base(absolute)
}
