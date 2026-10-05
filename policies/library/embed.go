package library

import (
	"embed"
	"io/fs"
)

//go:embed templates constraints tests
var embedded embed.FS

func FS() fs.FS {
	return embedded
}

func Files() (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(embedded, ".", func(path string, entry fs.DirEntry, err error) error {
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
		out[path] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
