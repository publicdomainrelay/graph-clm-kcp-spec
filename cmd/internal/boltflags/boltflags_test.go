package boltflags

import (
	"flag"
	"os"
	"testing"
)

var backendEnv = []string{
	"SPECD_BOLT_BACKEND",
	"SPECD_BOLT_URL",
	"SPECD_BOLT_USER",
	"SPECD_BOLT_PASSWORD",
	"SPECD_BOLT_PASSWORD_FILE",
	"SPECD_BOLT_DATABASE",
}

// unset removes the bolt environment for one test and restores it afterwards,
// so a test never depends on what the Makefile or the caller exported.
func unset(t *testing.T) {
	t.Helper()
	for _, name := range backendEnv {
		value, present := os.LookupEnv(name)
		if present {
			t.Cleanup(func() { os.Setenv(name, value) })
		}
		os.Unsetenv(name)
	}
}

func resolve(t *testing.T, args ...string) *Options {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	options := Add(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if err := options.Resolve(fs); err != nil {
		t.Fatal(err)
	}
	return options
}

func TestArcadeDBIsTheDefaultBackend(t *testing.T) {
	unset(t)
	options := resolve(t)
	if options.Backend != ArcadeDB {
		t.Errorf("backend = %q, want %q", options.Backend, ArcadeDB)
	}
	if options.URL != "bolt://127.0.0.1:7688" {
		t.Errorf("url = %q, want the ArcadeDB default", options.URL)
	}
	if options.User != "root" || options.Password != "clm-arcadedb-root" || options.Database != "clm" {
		t.Errorf("arcadedb defaults = %+v", options)
	}
	if options.PasswordFile != "" {
		t.Errorf("password file = %q, want none for ArcadeDB", options.PasswordFile)
	}
	if !options.Enabled() {
		t.Error("the graph is off by default")
	}
}

func TestHydraDBIsAnOptionThatBringsItsOwnDefaults(t *testing.T) {
	unset(t)
	options := resolve(t, "--bolt-backend", "hydradb")
	if options.URL != "bolt://127.0.0.1:7687" || options.User != "neo4j" {
		t.Errorf("hydradb defaults = %+v", options)
	}
	if options.PasswordFile != "/tmp/hdb/token" {
		t.Errorf("password file = %q, want the HydraDB token file", options.PasswordFile)
	}
	if options.Database != "" {
		t.Errorf("database = %q, want none for HydraDB", options.Database)
	}
}

func TestBackendEnvironmentSelectsTheDefaults(t *testing.T) {
	unset(t)
	t.Setenv("SPECD_BOLT_BACKEND", "hydradb")
	options := resolve(t)
	if options.Backend != HydraDB || options.URL != "bolt://127.0.0.1:7687" {
		t.Errorf("backend from the environment = %+v", options)
	}
}

func TestTheBackendFlagBeatsTheEnvironment(t *testing.T) {
	unset(t)
	t.Setenv("SPECD_BOLT_BACKEND", "hydradb")
	options := resolve(t, "--bolt-backend", "arcadedb")
	if options.Backend != ArcadeDB || options.URL != "bolt://127.0.0.1:7688" || options.User != "root" {
		t.Errorf("the flag did not beat the environment: %+v", options)
	}
}

func TestAnOptionEnvironmentBeatsTheBackendDefault(t *testing.T) {
	unset(t)
	t.Setenv("SPECD_BOLT_URL", "bolt://example:9999")
	t.Setenv("SPECD_BOLT_USER", "alice")
	t.Setenv("SPECD_BOLT_PASSWORD", "secret")
	t.Setenv("SPECD_BOLT_DATABASE", "other")
	options := resolve(t)
	if options.URL != "bolt://example:9999" || options.User != "alice" ||
		options.Password != "secret" || options.Database != "other" {
		t.Errorf("environment did not beat the backend defaults: %+v", options)
	}
}

func TestAnOptionFlagBeatsTheEnvironment(t *testing.T) {
	unset(t)
	t.Setenv("SPECD_BOLT_URL", "bolt://example:9999")
	t.Setenv("SPECD_BOLT_USER", "alice")
	options := resolve(t, "--bolt-url", "bolt://flag:1234", "--bolt-user", "bob")
	if options.URL != "bolt://flag:1234" || options.User != "bob" {
		t.Errorf("a flag must beat the environment: %+v", options)
	}
}

func TestAnEmptyURLEnvironmentTurnsTheGraphOff(t *testing.T) {
	unset(t)
	t.Setenv("SPECD_BOLT_URL", "")
	options := resolve(t)
	if options.URL != "" || options.Enabled() {
		t.Errorf("url = %q, want the graph off", options.URL)
	}
	if options.User != "root" {
		t.Errorf("user = %q, want the ArcadeDB default still applied", options.User)
	}
}

func TestAnUnknownBackendIsAnError(t *testing.T) {
	unset(t)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	options := Add(fs)
	if err := fs.Parse([]string{"--bolt-backend", "sqlite"}); err != nil {
		t.Fatal(err)
	}
	if err := options.Resolve(fs); err == nil {
		t.Error("an unknown backend was accepted")
	}
}

func TestHydraDBDefaultsCanBeOverriddenOneByOne(t *testing.T) {
	unset(t)
	t.Setenv("SPECD_BOLT_PASSWORD_FILE", "")
	options := resolve(t, "--bolt-backend", "hydradb", "--bolt-password", "secret")
	if options.Password != "secret" {
		t.Errorf("password = %q", options.Password)
	}
	if options.PasswordFile != "" {
		t.Errorf("password file = %q, want the explicit empty environment honoured", options.PasswordFile)
	}
	if options.URL != "bolt://127.0.0.1:7687" {
		t.Errorf("url = %q, want the HydraDB default", options.URL)
	}
}
