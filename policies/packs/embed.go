package packs

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed all:rfp-guest-isolation
//go:embed all:conformance
var embedded embed.FS

// FS is the registry of the packs embedded in specd and specctl. Adding a pack
// adds its directory to the go:embed line above.
func FS() fs.FS {
	return embedded
}

func Names() []string {
	entries, err := fs.ReadDir(embedded, ".")
	if err != nil {
		return nil
	}
	out := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Files is every file of one pack, keyed by its path inside the pack.
func Files(name string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(embedded, name, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := embedded.ReadFile(path)
		if err != nil {
			return err
		}
		out[strings.TrimPrefix(path, name+"/")] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
