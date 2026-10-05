package effects

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

const ClassifiersDir = "classifiers"

//go:embed packs/*.yaml
var packFS embed.FS

type Options struct {
	ClassifiersDirs []string

	IncludeExtras bool

	OnlyLanguage string
}

func Embedded() ([]policy.ClassifierPack, error) {
	names, err := fs.Glob(packFS, "packs/*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	packs := make([]policy.ClassifierPack, 0, len(names))
	for _, name := range names {
		data, err := packFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		pack, err := ParsePack(data)
		if err != nil {
			return nil, fmt.Errorf("effects: %s: %w", name, err)
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

func ParsePack(data []byte) (policy.ClassifierPack, error) {
	var pack policy.ClassifierPack
	if err := yaml.Unmarshal(data, &pack); err != nil {
		return policy.ClassifierPack{}, err
	}
	if strings.TrimSpace(pack.Language) == "" {
		return policy.ClassifierPack{}, fmt.Errorf("classifier pack has no language")
	}
	for _, rule := range pack.Rules {
		if strings.TrimSpace(rule.ID) == "" {
			return policy.ClassifierPack{}, fmt.Errorf("classifier pack %s has a rule without id", pack.Language)
		}
	}
	return pack, nil
}

func LoadDir(dir string) ([]policy.ClassifierPack, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	packs := make([]policy.ClassifierPack, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		pack, err := ParsePack(data)
		if err != nil {
			return nil, fmt.Errorf("effects: %s: %w", name, err)
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

func Packs(opts Options) ([]policy.ClassifierPack, error) {
	packs, err := Embedded()
	if err != nil {
		return nil, err
	}
	for _, dir := range opts.ClassifiersDirs {
		extra, err := LoadDir(dir)
		if err != nil {
			return nil, err
		}
		packs = append(packs, extra...)
	}
	if opts.OnlyLanguage != "" {
		filtered := packs[:0]
		for _, pack := range packs {
			if strings.EqualFold(pack.Language, opts.OnlyLanguage) {
				filtered = append(filtered, pack)
			}
		}
		packs = filtered
	}
	return packs, nil
}

func Compute(graph policy.CodeGraph, packs []policy.ClassifierPack, match policy.MatchOptions) ([]policy.Effect, error) {
	matcher, err := policy.Compile(packs...)
	if err != nil {
		return nil, err
	}
	return matcher.Effects(graph, match), nil
}

func Apply(graph *policy.CodeGraph, opts Options) ([]policy.Effect, error) {
	packs, err := Packs(opts)
	if err != nil {
		return nil, err
	}
	computed, err := Compute(*graph, packs, policy.MatchOptions{IncludeExtras: opts.IncludeExtras})
	if err != nil {
		return nil, err
	}
	graph.Spec.Effects = computed
	return computed, nil
}

// Dirs names the classifier packs a worktree adds to the embedded ones: its
// classifiers/ directory when it has one. Every caller that computes effects
// over a checkout uses it, so the audit, the gate and specctl see the same
// packs.
func Dirs(worktree string) []string {
	if worktree == "" {
		return nil
	}
	candidate := filepath.Join(worktree, ClassifiersDir)
	if _, err := os.Stat(candidate); err != nil {
		return nil
	}
	return []string{candidate}
}

func Counts(effects []policy.Effect) map[policy.EffectKind]int {
	counts := map[policy.EffectKind]int{}
	for _, effect := range effects {
		counts[effect.Kind]++
	}
	return counts
}
