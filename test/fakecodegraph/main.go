// Command fakecodegraph stands in for the codegraph indexer in tests that have
// no codegraph on PATH. It writes the sqlite index the readers expect: a file
// row and a file node for every source file under the root, walking the work
// tree the way the real indexer does, including the work trees of submodules.
package main

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	args := os.Args[1:]
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: codegraph init|sync [-y|-q] <root>")
		os.Exit(2)
	}
	root := args[len(args)-1]
	if err := index(root); err != nil {
		fmt.Fprintln(os.Stderr, "fakecodegraph:", err)
		os.Exit(1)
	}
}

func language(path string) string {
	switch filepath.Ext(path) {
	case ".ts":
		return "typescript"
	case ".go":
		return "go"
	case ".sh":
		return "shell"
	}
	return ""
}

func index(root string) error {
	directory := filepath.Join(root, ".codegraph")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	database := filepath.Join(directory, "codegraph.db")
	os.Remove(database)
	handle, err := sql.Open("sqlite", database)
	if err != nil {
		return err
	}
	defer handle.Close()
	for _, statement := range []string{
		`CREATE TABLE files (path TEXT, language TEXT)`,
		`CREATE TABLE nodes (id TEXT, kind TEXT, name TEXT, qualified_name TEXT, file_path TEXT, language TEXT, signature TEXT, start_line INTEGER, end_line INTEGER, is_exported INTEGER)`,
		`CREATE TABLE edges (source TEXT, target TEXT, kind TEXT, line INTEGER)`,
	} {
		if _, err := handle.Exec(statement); err != nil {
			return err
		}
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == ".codegraph" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		lang := language(name)
		if lang == "" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Count(string(data), "\n") + 1
		if _, err := handle.Exec(`INSERT INTO files VALUES (?, ?)`, relative, lang); err != nil {
			return err
		}
		_, err = handle.Exec(`INSERT INTO nodes VALUES (?, 'file', ?, ?, ?, ?, NULL, 1, ?, 0)`,
			"file:"+relative, name, relative, relative, lang, lines)
		return err
	})
}
