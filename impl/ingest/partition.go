package ingest

import (
	"io/fs"
	"path/filepath"
	"sort"

	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
)

var skippedDirectories = map[string]bool{
	".git":         true,
	".codegraph":   true,
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
}

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
