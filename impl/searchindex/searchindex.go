package searchindex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	_ "modernc.org/sqlite"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/search"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
)

const DatabaseName = "search.db"

func Path(root string) string {
	return filepath.Join(root, DatabaseName)
}

func InstanceRoot(kubeconfig string) string {
	absolute, err := filepath.Abs(kubeconfig)
	if err != nil {
		absolute = kubeconfig
	}
	return filepath.Dir(absolute)
}

type Reader interface {
	Select(ctx context.Context, label string, properties []string, filter map[string]any) ([]map[string]any, error)
}

type Cluster interface {
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)
}

type Index struct {
	db *sql.DB
}

const documentsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS documents USING fts5(
	id UNINDEXED,
	label UNINDEXED,
	context UNINDEXED,
	text
);`

func Open(root string) (*Index, error) {
	path := Path(root)
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("searchindex: create %s: %w", dir, err)
		}
	}
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("searchindex: open %s: %w", path, err)
	}
	handle.SetMaxOpenConns(1)
	if _, err := handle.Exec(documentsSchema); err != nil {
		handle.Close()
		return nil, fmt.Errorf("searchindex: create %s: %w", path, err)
	}
	return &Index{db: handle}, nil
}

func (i *Index) Close() error {
	return i.db.Close()
}

func (i *Index) Put(ctx context.Context, documents []search.Document) error {
	return i.write(ctx, documents, false)
}

func (i *Index) Rebuild(ctx context.Context, documents []search.Document) error {
	return i.write(ctx, documents, true)
}

func (i *Index) write(ctx context.Context, documents []search.Document, replace bool) error {
	transaction, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("searchindex: begin: %w", err)
	}
	defer transaction.Rollback()
	if replace {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM documents`); err != nil {
			return fmt.Errorf("searchindex: clear: %w", err)
		}
	}
	statement, err := transaction.PrepareContext(ctx,
		`INSERT INTO documents (id, label, context, text) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("searchindex: prepare: %w", err)
	}
	defer statement.Close()
	for _, document := range documents {
		if document.ID == 0 || strings.TrimSpace(document.Text) == "" {
			continue
		}
		if _, err := statement.ExecContext(ctx, document.ID, document.Label, document.Context, document.Text); err != nil {
			return fmt.Errorf("searchindex: index %d: %w", document.ID, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("searchindex: commit: %w", err)
	}
	return nil
}

func (i *Index) Search(ctx context.Context, terms string, limit int) ([]search.Hit, error) {
	query := search.Query(terms)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = search.DefaultLimit
	}
	rows, err := i.db.QueryContext(ctx,
		`SELECT id, label, context, text, bm25(documents) AS score FROM documents WHERE documents MATCH ? ORDER BY score LIMIT ?`,
		query, limit)
	if err != nil {
		return nil, fmt.Errorf("searchindex: search %q: %w", terms, err)
	}
	defer rows.Close()
	out := []search.Hit{}
	for rows.Next() {
		hit := search.Hit{}
		if err := rows.Scan(&hit.ID, &hit.Label, &hit.Context, &hit.Text, &hit.Score); err != nil {
			return nil, fmt.Errorf("searchindex: scan hit: %w", err)
		}
		hit.Score = -hit.Score
		out = append(out, hit)
	}
	return out, rows.Err()
}

type claimSource struct {
	label string

	properties []string

	context string

	text func(row map[string]any) string

	key string
}

var claimSources = []claimSource{
	{
		label:      graph.LabelContext,
		properties: []string{"name", "intent"},
		context:    "name",
		key:        "intent",
		text:       func(row map[string]any) string { return value(row, "name") + "\n" + value(row, "intent") },
	},
	{
		label:      graph.LabelRequirement,
		properties: []string{"context", "reqId", "level", "text"},
		context:    "context",
		key:        "reqId",
		text:       func(row map[string]any) string { return value(row, "level") + " " + value(row, "text") },
	},
	{
		label:      graph.LabelInterface,
		properties: []string{"context", "name", "kind", "signature"},
		context:    "context",
		key:        "name",
		text: func(row map[string]any) string {
			return value(row, "name") + " " + value(row, "kind") + " " + value(row, "signature")
		},
	},
	{
		label:      graph.LabelPiMemory,
		properties: []string{"title", "body", "session"},
		context:    "session",
		key:        "title",
		text:       func(row map[string]any) string { return value(row, "title") + "\n" + value(row, "body") },
	},
}

func Documents(ctx context.Context, reader Reader, repoPath string) ([]search.Document, error) {
	out := []search.Document{}
	if reader != nil {
		for _, source := range claimSources {
			rows, err := reader.Select(ctx, source.label, source.properties, nil)
			if err != nil {
				return nil, fmt.Errorf("searchindex: read %s: %w", source.label, err)
			}
			for _, row := range rows {
				text := source.text(row)
				if strings.TrimSpace(text) == "" {
					continue
				}
				out = append(out, search.Document{
					ID:      int64Value(row["id"]),
					Label:   source.label + ":" + value(row, source.key),
					Context: value(row, source.context),
					Text:    text,
				})
			}
		}
	}
	symbols, err := symbolDocuments(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	return append(out, symbols...), nil
}

func symbolDocuments(ctx context.Context, repoPath string) ([]search.Document, error) {
	if repoPath == "" {
		return nil, nil
	}
	database, found, err := codegraphsqlite.OpenRepo(repoPath)
	if err != nil || !found {
		return nil, err
	}
	defer database.Close()
	nodes, err := database.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	out := []search.Document{}
	for _, node := range nodes {
		text := strings.Join([]string{node.Name, node.QualifiedName, node.Kind, node.Signature}, " ")
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, search.Document{
			ID:      ids.Stable("symbol:" + node.ID),
			Label:   "symbol:" + node.Kind,
			Context: node.FilePath,
			Text:    text,
		})
	}
	return out, nil
}

func Findings(ctx context.Context, cluster Cluster, namespace string) ([]search.Document, error) {
	listed, err := cluster.List(ctx, specapi.FindingGVR, namespace)
	if err != nil {
		return nil, fmt.Errorf("searchindex: read findings: %w", err)
	}
	out := []search.Document{}
	for index := range listed.Items {
		item := &listed.Items[index]
		summary, _, _ := unstructured.NestedString(item.Object, "spec", "summary")
		context, _, _ := unstructured.NestedString(item.Object, "spec", "context")
		if strings.TrimSpace(summary) == "" {
			continue
		}
		out = append(out, search.Document{
			ID:      ids.Stable("finding:" + item.GetName()),
			Label:   "SpecFinding:" + item.GetName(),
			Context: context,
			Text:    summary,
		})
	}
	return out, nil
}

func value(row map[string]any, key string) string {
	found, ok := row[key]
	if !ok || found == nil {
		return ""
	}
	return fmt.Sprint(found)
}

func int64Value(found any) int64 {
	switch typed := found.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	}
	return ids.Stable(fmt.Sprint(found))
}
