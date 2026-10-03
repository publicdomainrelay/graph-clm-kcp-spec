// Package boltflags gives the spec binaries the same Bolt endpoint flags. The
// graph is an optional derived index, so every command that can write it takes
// the same five options and the same SPECD_BOLT_* environment.
package boltflags

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltgraph"
)

type Options struct {
	URL string

	User string

	Password string

	PasswordFile string

	Database string
}

func Add(fs *flag.FlagSet) *Options {
	options := &Options{}
	fs.StringVar(&options.URL, "bolt-url", os.Getenv("SPECD_BOLT_URL"), "bolt endpoint; empty skips the graph")
	fs.StringVar(&options.User, "bolt-user", envOr("SPECD_BOLT_USER", "neo4j"), "bolt user")
	fs.StringVar(&options.Password, "bolt-password", os.Getenv("SPECD_BOLT_PASSWORD"), "bolt password")
	fs.StringVar(&options.PasswordFile, "bolt-password-file", os.Getenv("SPECD_BOLT_PASSWORD_FILE"), "file holding the bolt password")
	fs.StringVar(&options.Database, "bolt-database", os.Getenv("SPECD_BOLT_DATABASE"), "bolt database, needed by ArcadeDB")
	return options
}

func (o *Options) Connect(ctx context.Context) (*boltgraph.Client, error) {
	password := o.Password
	if password == "" && o.PasswordFile != "" {
		contents, err := os.ReadFile(o.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("read bolt password file: %w", err)
		}
		password = strings.TrimSpace(string(contents))
	}
	return boltgraph.Connect(ctx, boltgraph.Options{
		URL:      o.URL,
		User:     o.User,
		Password: password,
		Database: o.Database,
	}, 3)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
