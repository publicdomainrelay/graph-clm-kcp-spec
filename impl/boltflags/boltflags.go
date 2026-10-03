// Package boltflags gives the spec binaries the same Bolt endpoint flags. The
// graph is a derived index, so every command that can write it takes the same
// options and the same SPECD_BOLT_* environment. One backend is selected
// (ArcadeDB by default, HydraDB by option) and it fills the options that no
// flag and no environment variable set.
package boltflags

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltgraph"
)

const (
	ArcadeDB = "arcadedb"

	HydraDB = "hydradb"

	DefaultBackend = ArcadeDB
)

// BackendDefaults is where one backend listens and how it authenticates. The
// values describe the local development instances; a flag or an environment
// variable overrides any of them.
type BackendDefaults struct {
	URL string

	User string

	Password string

	PasswordFile string

	Database string
}

var backendDefaults = map[string]BackendDefaults{
	ArcadeDB: {
		URL:      "bolt://127.0.0.1:7688",
		User:     "root",
		Password: "clm-arcadedb-root",
		Database: "clm",
	},
	HydraDB: {
		URL:          "bolt://127.0.0.1:7687",
		User:         "neo4j",
		PasswordFile: "/tmp/hdb/token",
	},
}

type Options struct {
	Backend string

	URL string

	User string

	Password string

	PasswordFile string

	Database string
}

func Add(fs *flag.FlagSet) *Options {
	options := &Options{}
	fs.StringVar(&options.Backend, "bolt-backend", envOr("SPECD_BOLT_BACKEND", DefaultBackend),
		"graph backend whose defaults the unset options take: arcadedb or hydradb")
	fs.StringVar(&options.URL, "bolt-url", "", "bolt endpoint; empty skips the graph (default: the backend's)")
	fs.StringVar(&options.User, "bolt-user", "", "bolt user (default: the backend's)")
	fs.StringVar(&options.Password, "bolt-password", "", "bolt password (default: the backend's)")
	fs.StringVar(&options.PasswordFile, "bolt-password-file", "", "file holding the bolt password (default: the backend's)")
	fs.StringVar(&options.Database, "bolt-database", "", "bolt database, needed by ArcadeDB (default: the backend's)")
	return options
}

// Resolve fills the options no flag and no environment variable set from the
// selected backend: a flag beats an environment variable beats the backend
// default. An environment variable that is set but empty is a value, so
// `SPECD_BOLT_URL=` turns the graph off for a whole run.
func (o *Options) Resolve(fs *flag.FlagSet) error {
	backend := strings.ToLower(strings.TrimSpace(o.Backend))
	defaults, ok := backendDefaults[backend]
	if !ok {
		return fmt.Errorf("boltflags: %q is not %s or %s", o.Backend, ArcadeDB, HydraDB)
	}
	o.Backend = backend
	set := map[string]bool{}
	fs.Visit(func(parsed *flag.Flag) { set[parsed.Name] = true })
	o.URL = pick(set["bolt-url"], "SPECD_BOLT_URL", o.URL, defaults.URL)
	o.User = pick(set["bolt-user"], "SPECD_BOLT_USER", o.User, defaults.User)
	o.Password = pick(set["bolt-password"], "SPECD_BOLT_PASSWORD", o.Password, defaults.Password)
	o.PasswordFile = pick(set["bolt-password-file"], "SPECD_BOLT_PASSWORD_FILE", o.PasswordFile, defaults.PasswordFile)
	o.Database = pick(set["bolt-database"], "SPECD_BOLT_DATABASE", o.Database, defaults.Database)
	return nil
}

func (o *Options) Enabled() bool {
	return o.URL != ""
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

func pick(fromFlag bool, envName, flagValue, fallback string) string {
	if fromFlag {
		return flagValue
	}
	if value, ok := os.LookupEnv(envName); ok {
		return value
	}
	return fallback
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
