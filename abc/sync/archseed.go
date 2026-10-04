package specsync

import (
	"slices"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/archyaml"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

type archScore struct {
	named int

	inside int

	outside int

	depth int
}

func (s archScore) better(than archScore) bool {
	if s.named != than.named {
		return s.named > than.named
	}
	if s.inside != than.inside {
		return s.inside > than.inside
	}
	if s.outside != than.outside {
		return s.outside < than.outside
	}
	return s.depth < than.depth
}

func MatchArch(partitions []Partition, document *archyaml.Document) map[string]string {
	if document == nil {
		return nil
	}
	candidates := []*archyaml.Node{}
	for _, node := range document.Nodes() {
		if len(node.Code) > 0 {
			candidates = append(candidates, node)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	depths := nodeDepths(document)
	matched := map[string]string{}
	for _, partition := range partitions {
		best := archScore{}
		bestID := ""
		for _, node := range candidates {
			score := archMatchScore(partition, node, depths[node.ID])
			if score.named == 0 && score.inside == 0 {
				continue
			}
			if bestID == "" || score.better(best) || (score == best && node.ID < bestID) {
				best = score
				bestID = node.ID
			}
		}
		if bestID != "" {
			matched[partition.Name] = bestID
		}
	}
	return matched
}

func RootArchNodeID(document *archyaml.Document, repositoryName string) string {
	if document == nil || repositoryName == "" {
		return ""
	}
	ids := []string{}
	for _, node := range document.Nodes() {
		if node.Parent != "" {
			continue
		}
		ids = append(ids, node.ID)
	}
	sort.Strings(ids)
	want := spec.ArchName(repositoryName)
	for _, id := range ids {
		payload, ok := spec.RefName(id)
		if !ok {
			payload = id
			if index := strings.Index(id, "."); index >= 0 {
				payload = id[index+1:]
			}
		}
		if spec.ArchName(payload) == want {
			return id
		}
	}
	return ""
}

func nodeDepths(document *archyaml.Document) map[string]int {
	out := map[string]int{}
	for _, node := range document.Nodes() {
		depth := 0
		current := node
		for current.Parent != "" {
			parent := document.Node(current.Parent)
			if parent == nil {
				break
			}
			depth++
			current = parent
		}
		out[node.ID] = depth
	}
	return out
}

func archMatchScore(partition Partition, node *archyaml.Node, depth int) archScore {
	score := archScore{depth: depth}
	for _, path := range node.Code {
		trimmed := strings.TrimPrefix(strings.TrimSpace(path), "./")
		if trimmed == "" {
			continue
		}
		switch {
		case trimmed == partition.Directory, strings.HasPrefix(trimmed, partition.Directory+"/"):
			score.inside++
			if slices.Contains(partition.Files, trimmed) || slices.Contains(partition.TreeFiles, trimmed) {
				score.named++
			}
		case strings.HasPrefix(partition.Directory, trimmed+"/"):
			score.inside++
		default:
			score.outside++
		}
	}
	return score
}
