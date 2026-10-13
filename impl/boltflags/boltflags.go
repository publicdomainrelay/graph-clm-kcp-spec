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

	DefaultBackend = HydraDB
)

type BackendDefaults struct {
	URL string

	User string

	Password string

	PasswordFile string

	Database string
}

var backendDefaults = map[string]BackendDefaults{
	ArcadeDB: {
		User:     "root",
		Password: "clm-arcadedb-root",
		Database: "clm",
	},
	HydraDB: {
		User: "neo4j",
	},
}

// Instance is the graph a running instance reports: specctl up records the
// graph-node it started in the session, and Resolve takes it when no flag and
// no environment variable names a graph.
type Instance struct {
	Backend string

	URL string

	User string

	PasswordFile string
}

// SessionGraph returns the graph of the instance the command runs in. The
// command sets it; a library leaves it nil.
var SessionGraph func() (Instance, bool)

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
	fs.StringVar(&options.URL, "bolt-url", "", "bolt endpoint of an external graph (default: the graph the instance started)")
	fs.StringVar(&options.User, "bolt-user", "", "bolt user (default: the backend's)")
	fs.StringVar(&options.Password, "bolt-password", "", "bolt password (default: the backend's)")
	fs.StringVar(&options.PasswordFile, "bolt-password-file", "", "file holding the bolt password (default: the backend's)")
	fs.StringVar(&options.Database, "bolt-database", "", "bolt database, needed by ArcadeDB (default: the backend's)")
	return options
}

func (o *Options) Resolve(fs *flag.FlagSet) error {
	set := map[string]bool{}
	fs.Visit(func(parsed *flag.Flag) { set[parsed.Name] = true })
	if !named(set["bolt-url"], "SPECD_BOLT_URL") && SessionGraph != nil {
		if instance, ok := SessionGraph(); ok && instance.URL != "" {
			o.fromInstance(set, instance)
		}
	}
	backend := strings.ToLower(strings.TrimSpace(o.Backend))
	defaults, ok := backendDefaults[backend]
	if !ok {
		return fmt.Errorf("boltflags: %q is not %s or %s", o.Backend, ArcadeDB, HydraDB)
	}
	o.Backend = backend
	o.URL = pick(set["bolt-url"], "SPECD_BOLT_URL", o.URL, "")
	o.User = pick(set["bolt-user"], "SPECD_BOLT_USER", o.User, defaults.User)
	o.Password = pick(set["bolt-password"], "SPECD_BOLT_PASSWORD", o.Password, defaults.Password)
	o.PasswordFile = pick(set["bolt-password-file"], "SPECD_BOLT_PASSWORD_FILE", o.PasswordFile, defaults.PasswordFile)
	o.Database = pick(set["bolt-database"], "SPECD_BOLT_DATABASE", o.Database, defaults.Database)
	return nil
}

func (o *Options) fromInstance(set map[string]bool, instance Instance) {
	o.URL = instance.URL
	set["bolt-url"] = true
	if !named(set["bolt-backend"], "SPECD_BOLT_BACKEND") && instance.Backend != "" {
		o.Backend = instance.Backend
	}
	if !named(set["bolt-user"], "SPECD_BOLT_USER") && instance.User != "" {
		o.User = instance.User
		set["bolt-user"] = true
	}
	if !named(set["bolt-password-file"], "SPECD_BOLT_PASSWORD_FILE") && instance.PasswordFile != "" {
		o.PasswordFile = instance.PasswordFile
		set["bolt-password-file"] = true
	}
}

func named(fromFlag bool, envName string) bool {
	if fromFlag {
		return true
	}
	value, ok := os.LookupEnv(envName)
	return ok && value != ""
}

// Enabled reports whether a graph is named. When none is, a long-running
// process starts its own (graphproc) and a one-shot command runs without one.
func (o *Options) Enabled() bool {
	return o.URL != ""
}

// Use points the options at a graph an instance started.
func (o *Options) Use(instance Instance) {
	o.Backend = instance.Backend
	o.URL = instance.URL
	o.User = instance.User
	o.Password = ""
	o.PasswordFile = instance.PasswordFile
	o.Database = backendDefaults[instance.Backend].Database
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
