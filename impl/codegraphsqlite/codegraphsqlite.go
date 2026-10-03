package codegraphsqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
)

const (
	Directory    = ".codegraph"
	DatabaseName = "codegraph.db"
	DefaultTool  = "codegraph"
)

type File struct {
	Path     string
	Language string
}

type Node struct {
	ID            string
	Kind          string
	Name          string
	QualifiedName string
	FilePath      string
	Language      string
	Signature     string
	StartLine     int
	EndLine       int
	IsExported    bool
}

type Edge struct {
	Source string
	Target string
	Kind   string
	Line   int
}

type DB struct {
	sql *sql.DB
}

func DatabasePath(repoPath string) string {
	return filepath.Join(repoPath, Directory, DatabaseName)
}

// Ensure indexes the repository when that is needed and returns the path of
// the sqlite database codegraph wrote. An existing index is synced so the
// facts match the working tree.
func Ensure(ctx context.Context, repoPath, tool string) (string, error) {
	if tool == "" {
		tool = DefaultTool
	}
	if _, err := exec.LookPath(tool); err != nil {
		return "", fmt.Errorf("codegraphsqlite: %s is not on PATH: %w", tool, err)
	}
	dbPath := DatabasePath(repoPath)
	action := "sync"
	args := []string{"sync", "-q"}
	if _, err := os.Stat(dbPath); err != nil {
		action = "init"
		args = []string{"init", "-y"}
	}
	args = append(args, repoPath)
	command := exec.CommandContext(ctx, tool, args...)
	command.Dir = repoPath
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("codegraphsqlite: %s %s: %w: %s", tool, action, err, strings.TrimSpace(string(output)))
	}
	if _, err := os.Stat(dbPath); err != nil {
		return "", fmt.Errorf("codegraphsqlite: %s did not create %s: %w", action, dbPath, err)
	}
	return dbPath, nil
}

// OpenRepo opens the index of a repository that has already been indexed. It
// reports found as false when the repository has no index yet.
func OpenRepo(repoPath string) (*DB, bool, error) {
	dbPath := DatabasePath(repoPath)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, false, nil
	}
	database, err := Open(dbPath)
	if err != nil {
		return nil, false, err
	}
	return database, true, nil
}

func Open(dbPath string) (*DB, error) {
	handle, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("codegraphsqlite: open %s: %w", dbPath, err)
	}
	handle.SetMaxOpenConns(1)
	if err := handle.Ping(); err != nil {
		handle.Close()
		return nil, fmt.Errorf("codegraphsqlite: ping %s: %w", dbPath, err)
	}
	return &DB{sql: handle}, nil
}

func (db *DB) Close() error {
	return db.sql.Close()
}

func (db *DB) Files(ctx context.Context) ([]File, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT path, language FROM files ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("codegraphsqlite: read files: %w", err)
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		file := File{}
		if err := rows.Scan(&file.Path, &file.Language); err != nil {
			return nil, fmt.Errorf("codegraphsqlite: scan file: %w", err)
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

const nodeColumns = `id, kind, name, qualified_name, file_path, language, signature, start_line, end_line, is_exported`

func (db *DB) Nodes(ctx context.Context) ([]Node, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+nodeColumns+` FROM nodes ORDER BY file_path, start_line, name`)
	if err != nil {
		return nil, fmt.Errorf("codegraphsqlite: read nodes: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (db *DB) NodesForFile(ctx context.Context, file string) ([]Node, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT `+nodeColumns+` FROM nodes WHERE file_path = ? ORDER BY start_line, name`, file)
	if err != nil {
		return nil, fmt.Errorf("codegraphsqlite: read nodes for %s: %w", file, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
	out := []Node{}
	for rows.Next() {
		node := Node{}
		var signature sql.NullString
		if err := rows.Scan(
			&node.ID, &node.Kind, &node.Name, &node.QualifiedName, &node.FilePath,
			&node.Language, &signature, &node.StartLine, &node.EndLine, &node.IsExported,
		); err != nil {
			return nil, fmt.Errorf("codegraphsqlite: scan node: %w", err)
		}
		node.Signature = signature.String
		out = append(out, node)
	}
	return out, rows.Err()
}

func (db *DB) Edges(ctx context.Context) ([]Edge, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT source, target, kind, COALESCE(line, 0) FROM edges ORDER BY source, target, kind`)
	if err != nil {
		return nil, fmt.Errorf("codegraphsqlite: read edges: %w", err)
	}
	defer rows.Close()
	out := []Edge{}
	for rows.Next() {
		edge := Edge{}
		if err := rows.Scan(&edge.Source, &edge.Target, &edge.Kind, &edge.Line); err != nil {
			return nil, fmt.Errorf("codegraphsqlite: scan edge: %w", err)
		}
		out = append(out, edge)
	}
	return out, rows.Err()
}

// Facts maps the index onto the pure sync input. Only exported nodes become
// symbols; the partition step decides which kinds count as an interface.
func (db *DB) Facts(ctx context.Context, commit string) (specsync.Facts, error) {
	files, err := db.Files(ctx)
	if err != nil {
		return specsync.Facts{}, err
	}
	nodes, err := db.Nodes(ctx)
	if err != nil {
		return specsync.Facts{}, err
	}
	facts := specsync.Facts{Commit: commit}
	for _, file := range files {
		facts.Files = append(facts.Files, specsync.SourceFile{Path: file.Path, Language: file.Language})
	}
	for _, node := range nodes {
		if !node.IsExported {
			continue
		}
		facts.Symbols = append(facts.Symbols, specsync.Symbol{
			ID:        node.ID,
			Name:      node.Name,
			Kind:      node.Kind,
			Signature: node.Signature,
			File:      node.FilePath,
			Line:      node.StartLine,
			Exported:  true,
		})
	}
	return facts, nil
}

// Resolve turns a code ref payload into codegraph nodes. It never computes an
// id: it looks the payload up as a codegraph id, then as a qualified name,
// then as a bare name, and finally as a file path.
func (db *DB) Resolve(ctx context.Context, reference string) ([]Node, error) {
	if nodes, err := db.nodesByID(ctx, reference); err != nil {
		return nil, err
	} else if len(nodes) > 0 {
		return nodes, nil
	}
	if nodes, err := db.query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE qualified_name = ? LIMIT 2`, reference); err != nil {
		return nil, err
	} else if len(nodes) > 0 {
		return nodes, nil
	}
	if strings.ContainsAny(reference, "/\\") {
		if nodes, err := db.nodesByFilePath(ctx, reference); err != nil {
			return nil, err
		} else if len(nodes) > 0 {
			return nodes, nil
		}
	}
	if nodes, err := db.query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE name = ? LIMIT 2`, reference); err != nil {
		return nil, err
	} else if len(nodes) > 0 {
		return nodes, nil
	}
	if index := strings.LastIndex(reference, "."); index >= 0 {
		return db.query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE name = ? LIMIT 2`, reference[index+1:])
	}
	return nil, nil
}

func (db *DB) nodesByID(ctx context.Context, id string) ([]Node, error) {
	return db.query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = ?`, id)
}

func (db *DB) nodesByFilePath(ctx context.Context, filePath string) ([]Node, error) {
	cleaned := strings.TrimPrefix(filepath.ToSlash(filePath), "./")
	return db.query(ctx,
		`SELECT `+nodeColumns+` FROM nodes WHERE kind = 'file' AND file_path = ? LIMIT 1`, cleaned)
}

func (db *DB) query(ctx context.Context, statement string, args ...any) ([]Node, error) {
	rows, err := db.sql.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("codegraphsqlite: query: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}
