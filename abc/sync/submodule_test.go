package specsync

import "testing"

// An org root excludes "<submodule>/**" for every gitlink. The partitioner must
// give the root contexts for its own directories and none for a member's.
func TestSubmoduleExcludesKeepMemberFilesOutOfTheRootsContexts(t *testing.T) {
	facts := Facts{Files: []SourceFile{
		{Path: "scripts/find.ts", Language: "typescript"},
		{Path: "docs/notes.ts", Language: "typescript"},
		{Path: "market/lib/requester/mod.ts", Language: "typescript"},
		{Path: "relay/lib/relay-server/mod.ts", Language: "typescript"},
	}}
	partitions := PartitionFactsWith(facts, PartitionOptions{
		RepositoryName: "root",
		Exclude:        []string{"market/**", "relay/**"},
	})
	seen := map[string]bool{}
	for _, partition := range partitions {
		seen[partition.Name] = true
		for _, file := range partition.Files {
			if len(file) >= 7 && (file[:7] == "market/" || file[:6] == "relay/") {
				t.Fatalf("partition %s holds %s", partition.Name, file)
			}
		}
	}
	if len(partitions) != 2 {
		t.Fatalf("partitions = %v, want one each for scripts and docs", seen)
	}
}
