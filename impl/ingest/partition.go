package ingest

import (
	"io/fs"
	"path/filepath"
	"sort"

	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
)

// skippedDirectories are the trees a package partition never descends into: a
// version control store, the index this repository writes, and the dependency
// trees a package manifest may itself live in.
var skippedDirectories = map[string]bool{
	".git":         true,
	".codegraph":   true,
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
}

// PackageRoots lists the directories of a working tree that hold a package
// manifest, plus the tree root, as repository-relative slash paths. The package
// partition groups by them; the manifest itself is never indexed, so this is
// the only place that can see it.
func PackageRoots(repoPath string) ([]string, error) {
	roots := map[string]bool{".": true}
	err := filepath.WalkDir(repoPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != repoPath && skippedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		for _, manifest := range specsync.PackageManifests {
			if entry.Name() != manifest {
				continue
			}
			relative, err := filepath.Rel(repoPath, filepath.Dir(path))
			if err != nil {
				return err
			}
			roots[filepath.ToSlash(relative)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(roots))
	for root := range roots {
		out = append(out, root)
	}
	sort.Strings(out)
	return out, nil
}
