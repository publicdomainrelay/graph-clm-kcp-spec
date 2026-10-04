package boltgraph

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
)

type Options struct {
	URL      string
	User     string
	Password string
	Database string
}

type Client struct {
	driver   neo4j.DriverWithContext
	database string
}

func Connect(ctx context.Context, options Options, attempts int) (*Client, error) {
	if options.URL == "" {
		return nil, errors.New("boltgraph: a bolt url is required")
	}
	user := options.User
	if user == "" {
		user = "neo4j"
	}
	if attempts <= 0 {
		attempts = 3
	}
	var lastError error
	for attempt := 0; attempt < attempts; attempt++ {
		driver, err := neo4j.NewDriverWithContext(options.URL, neo4j.BasicAuth(user, options.Password, ""))
		if err != nil {
			lastError = err
			continue
		}
		if err := driver.VerifyConnectivity(ctx); err != nil {
			lastError = err
			_ = driver.Close(ctx)
			continue
		}
		return &Client{driver: driver, database: options.Database}, nil
	}
	return nil, fmt.Errorf("boltgraph: connect %s: %w", options.URL, lastError)
}

func (c *Client) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

func (c *Client) session(ctx context.Context) neo4j.SessionWithContext {
	return c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.database})
}

func (c *Client) Run(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	session := c.session(ctx)
	defer session.Close(ctx)
	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("boltgraph: run: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("boltgraph: collect: %w", err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := map[string]any{}
		for key, value := range record.AsMap() {
			row[key] = plain(value)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

const writeBatch = 64

func (c *Client) WriteVertices(ctx context.Context, set graph.VertexSet) error {
	if len(set.Rows) == 0 {
		return nil
	}
	properties := graph.LabelProperties[set.Label]
	query := graph.VertexUpsert(set.Label, properties)
	rows := make([]map[string]any, 0, len(set.Rows))
	for _, vertex := range set.Rows {
		rows = append(rows, vertex.Row())
	}
	return c.writeBatched(ctx, query, rows)
}

func (c *Client) WriteEdges(ctx context.Context, set graph.EdgeSet) error {
	if len(set.Rows) == 0 {
		return nil
	}
	query := graph.EdgeCreate(set.Type, set.FromLabel, set.ToLabel)
	rows := make([]map[string]any, 0, len(set.Rows))
	for _, edge := range set.Rows {
		rows = append(rows, edge.Row())
	}
	return c.writeBatched(ctx, query, rows)
}

func (c *Client) DeleteVertices(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"id": id})
	}
	return c.writeBatched(ctx, graph.VertexDelete(), rows)
}

func (c *Client) writeBatched(ctx context.Context, query string, rows []map[string]any) error {
	for start := 0; start < len(rows); start += writeBatch {
		end := start + writeBatch
		if end > len(rows) {
			end = len(rows)
		}
		if _, err := c.Run(ctx, query, map[string]any{"rows": rows[start:end]}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) SelectIDs(ctx context.Context, label string) ([]int64, error) {
	rows, err := c.Run(ctx, graph.VertexIDs(label), nil)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if id, ok := toInt64(row["id"]); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *Client) SelectIDsWhere(ctx context.Context, label string, filter map[string]any) ([]int64, error) {
	rows, err := c.Run(ctx, graph.VertexSelect(label, []string{"id"}, filter), nil)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if id, ok := toInt64(row["id"]); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *Client) Select(ctx context.Context, label string, properties []string, filter map[string]any) ([]map[string]any, error) {
	return c.Run(ctx, graph.VertexSelect(label, properties, filter), nil)
}

func (c *Client) SelectOut(ctx context.Context, edgeType, fromLabel, toLabel string, fromID int64, properties []string) ([]map[string]any, error) {
	return c.Run(ctx, graph.EdgeSelectOut(edgeType, fromLabel, toLabel, fromID, properties), nil)
}

func (c *Client) SelectIn(ctx context.Context, edgeType, fromLabel, toLabel string, toID int64, properties []string) ([]map[string]any, error) {
	return c.Run(ctx, graph.EdgeSelectIn(edgeType, fromLabel, toLabel, toID, properties), nil)
}

func plain(value any) any {
	switch typed := value.(type) {
	case int64:
		return typed
	case []any:
		out := make([]any, 0, len(typed))
		for _, entry := range typed {
			out = append(out, plain(entry))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, entry := range typed {
			out[key] = plain(entry)
		}
		return out
	}
	return value
}

func toInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		if typed > math.MaxInt64 || typed < math.MinInt64 {
			return 0, false
		}
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}
