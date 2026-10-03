package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltgraph"
)

type boltOptions struct {
	url          string
	user         string
	password     string
	passwordFile string
	database     string
}

func addBolt(fs *flag.FlagSet) *boltOptions {
	options := &boltOptions{}
	fs.StringVar(&options.url, "bolt-url", os.Getenv("SPECD_BOLT_URL"), "bolt endpoint; empty skips the graph")
	fs.StringVar(&options.user, "bolt-user", envOr("SPECD_BOLT_USER", "neo4j"), "bolt user")
	fs.StringVar(&options.password, "bolt-password", os.Getenv("SPECD_BOLT_PASSWORD"), "bolt password")
	fs.StringVar(&options.passwordFile, "bolt-password-file", os.Getenv("SPECD_BOLT_PASSWORD_FILE"), "file holding the bolt password")
	fs.StringVar(&options.database, "bolt-database", os.Getenv("SPECD_BOLT_DATABASE"), "bolt database, needed by ArcadeDB")
	return options
}

func (b *boltOptions) connect(ctx context.Context) (*boltgraph.Client, error) {
	password := b.password
	if password == "" && b.passwordFile != "" {
		contents, err := os.ReadFile(b.passwordFile)
		if err != nil {
			return nil, fmt.Errorf("read bolt password file: %w", err)
		}
		password = strings.TrimSpace(string(contents))
	}
	return boltgraph.Connect(ctx, boltgraph.Options{
		URL:      b.url,
		User:     b.user,
		Password: password,
		Database: b.database,
	}, 3)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
