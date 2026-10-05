package codegraphsqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

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

	root string
}

func DatabasePath(repoPath string) string {
	return filepath.Join(repoPath, Directory, DatabaseName)
}

func Ensure(ctx context.Context, repoPath, tool string) (string, error) {
	if tool == "" {
		tool = DefaultTool
	}
	if _, err := exec.LookPath(tool); err != nil {
		return "", fmt.Errorf("codegraphsqlite: %s is not on PATH: %w", tool, err)
	}
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return "", fmt.Errorf("codegraphsqlite: resolve %s: %w", repoPath, err)
	}
	dbPath := DatabasePath(root)
	action := "sync"
	args := []string{"sync", "-q"}
	if _, err := os.Stat(dbPath); err != nil {
		action = "init"
		args = []string{"init", "-y"}
	}
	args = append(args, root)
	command := exec.CommandContext(ctx, tool, args...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("codegraphsqlite: %s %s: %w: %s", tool, action, err, strings.TrimSpace(string(output)))
	}
	if _, err := os.Stat(dbPath); err != nil {
		return "", fmt.Errorf("codegraphsqlite: %s did not create %s: %w", action, dbPath, err)
	}
	if err := excludeLocally(ctx, root, Directory+"/"); err != nil {
		return "", err
	}
	return dbPath, nil
}

func excludeLocally(ctx context.Context, repoPath, pattern string) error {
	out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return nil
	}
	exclude := strings.TrimSpace(string(out))
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(repoPath, exclude)
	}
	existing, err := os.ReadFile(exclude)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("codegraphsqlite: read %s: %w", exclude, err)
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(exclude, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("codegraphsqlite: open %s: %w", exclude, err)
	}
	defer file.Close()
	prefix := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}
	_, err = file.WriteString(prefix + pattern + "\n")
	return err
}

func OpenRepo(repoPath string) (*DB, bool, error) {
	dbPath := DatabasePath(repoPath)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, false, nil
	}
	database, err := Open(dbPath)
	if err != nil {
		return nil, false, err
	}
	database.root = repoPath
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
	return &DB{sql: handle, root: filepath.Dir(filepath.Dir(dbPath))}, nil
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
		node.IsExported = node.IsExported || exportedMethod(node)
		out = append(out, node)
	}
	return out, rows.Err()
}

func exportedMethod(node Node) bool {
	if node.Kind != "method" || node.Language != "go" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(node.Name)
	return unicode.IsUpper(first)
}

func (db *DB) Imports(ctx context.Context) ([]specsync.Import, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT n.file_path, target.name
		FROM edges e
		JOIN nodes n ON n.id = e.source
		JOIN nodes target ON target.id = e.target
		WHERE e.kind = 'imports' AND n.kind = 'file'
		UNION
		SELECT n.file_path, target.name
		FROM edges e
		JOIN nodes n ON n.id = e.source
		JOIN nodes target ON target.id = e.target
		WHERE e.kind = 'contains' AND n.kind = 'file' AND target.kind = 'import'
		ORDER BY 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("codegraphsqlite: read imports: %w", err)
	}
	defer rows.Close()
	out := []specsync.Import{}
	for rows.Next() {
		entry := specsync.Import{}
		if err := rows.Scan(&entry.From, &entry.Path); err != nil {
			return nil, fmt.Errorf("codegraphsqlite: scan import: %w", err)
		}
		out = append(out, entry)
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

func (db *DB) Facts(ctx context.Context, commit string) (specsync.Facts, error) {
	files, err := db.Files(ctx)
	if err != nil {
		return specsync.Facts{}, err
	}
	nodes, err := db.Nodes(ctx)
	if err != nil {
		return specsync.Facts{}, err
	}
	imports, err := db.Imports(ctx)
	if err != nil {
		return specsync.Facts{}, err
	}
	facts := specsync.Facts{Commit: commit, Imports: imports}
	for _, file := range files {
		facts.Files = append(facts.Files, specsync.SourceFile{Path: file.Path, Language: file.Language})
	}
	types := exportedTypes(nodes)
	sources := map[string][]string{}
	for _, node := range nodes {
		if !db.exported(node, types, sources) {
			continue
		}
		facts.Symbols = append(facts.Symbols, specsync.Symbol{
			ID:        node.ID,
			Name:      node.Name,
			Qualified: qualifiedName(node),
			Kind:      node.Kind,
			Signature: node.Signature,
			File:      node.FilePath,
			Line:      node.StartLine,
			Exported:  true,
		})
	}
	return facts, nil
}

var typeKinds = map[string]bool{
	"class": true, "interface": true, "struct": true,
	"enum": true, "type_alias": true,
}

func exportedTypes(nodes []Node) map[string]bool {
	out := map[string]bool{}
	for _, node := range nodes {
		if node.IsExported && typeKinds[node.Kind] {
			out[node.FilePath+"\x00"+node.Name] = true
		}
	}
	return out
}

func qualifiedName(node Node) string {
	qualified := strings.TrimSpace(node.QualifiedName)
	if qualified == "" {
		return node.Name
	}
	if index := strings.Index(qualified, "::"); index >= 0 {
		return qualified[:index] + "." + qualified[index+2:]
	}
	return qualified
}

func (db *DB) exported(node Node, types map[string]bool, sources map[string][]string) bool {
	if node.IsExported {
		return true
	}
	if exportedMethod(node) {
		return true
	}
	if node.Language == "typescript" && node.Kind == "method" {
		return db.exportedTypeScriptMethod(node, types, sources)
	}
	return false
}

func (db *DB) exportedTypeScriptMethod(node Node, types map[string]bool, sources map[string][]string) bool {
	if strings.HasPrefix(node.Name, "#") {
		return false
	}
	parent := qualifiedName(node)
	if index := strings.LastIndex(parent, "."); index >= 0 {
		parent = parent[:index]
	}
	if parent != "" && !types[node.FilePath+"\x00"+parent] {
		return false
	}
	return typescriptMemberPublic(db.sourceLine(node, sources), node.Name)
}

func (db *DB) sourceLine(node Node, sources map[string][]string) string {
	lines, ok := sources[node.FilePath]
	if !ok {
		data, err := os.ReadFile(filepath.Join(db.root, filepath.FromSlash(node.FilePath)))
		if err != nil {
			lines = nil
		} else {
			lines = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		}
		sources[node.FilePath] = lines
	}
	if node.StartLine <= 0 || node.StartLine > len(lines) {
		return ""
	}
	return lines[node.StartLine-1]
}

func typescriptMemberPublic(line, name string) bool {
	index := tokenIndex(line, name)
	if index < 0 {
		return true
	}
	prefix := line[:index]
	for _, modifier := range []string{"private", "protected"} {
		if containsWord(prefix, modifier) {
			return false
		}
	}
	return true
}

func tokenIndex(line, name string) int {
	from := 0
	for from <= len(line) {
		index := strings.Index(line[from:], name)
		if index < 0 {
			return -1
		}
		index += from
		before := byte(' ')
		if index > 0 {
			before = line[index-1]
		}
		after := byte(' ')
		if end := index + len(name); end < len(line) {
			after = line[end]
		}
		if !identifierByte(before) && !identifierByte(after) {
			return index
		}
		from = index + 1
	}
	return -1
}

func identifierByte(char byte) bool {
	switch {
	case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		return true
	case char == '_', char == '$', char == '#':
		return true
	}
	return false
}

func containsWord(text, word string) bool {
	for from := 0; from <= len(text)-len(word); {
		index := strings.Index(text[from:], word)
		if index < 0 {
			return false
		}
		index += from
		before := byte(' ')
		if index > 0 {
			before = text[index-1]
		}
		after := byte(' ')
		if end := index + len(word); end < len(text) {
			after = text[end]
		}
		if !identifierByte(before) && !identifierByte(after) {
			return true
		}
		from = index + 1
	}
	return false
}

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
	if index := strings.LastIndex(reference, "."); index >= 0 {
		spelling := reference[:index] + "::" + reference[index+1:]
		if nodes, err := db.query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE qualified_name = ? LIMIT 2`, spelling); err != nil {
			return nil, err
		} else if len(nodes) > 0 {
			return nodes, nil
		}
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
