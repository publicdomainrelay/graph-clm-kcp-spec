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

func TestHydraDBIsTheDefaultAndNoGraphIsNamed(t *testing.T) {
	unset(t)
	options := resolve(t)
	if options.Backend != HydraDB || options.User != "neo4j" {
		t.Errorf("defaults = %+v", options)
	}
	if options.URL != "" || options.Enabled() {
		t.Errorf("url = %q: no fixed port is assumed; an instance starts its own graph", options.URL)
	}
	if options.PasswordFile != "" {
		t.Errorf("password file = %q, want none", options.PasswordFile)
	}
}

func TestArcadeDBIsAnExternalOptionWithItsOwnDefaults(t *testing.T) {
	unset(t)
	options := resolve(t, "--bolt-backend", "arcadedb", "--bolt-url", "bolt://127.0.0.1:7688")
	if options.Backend != ArcadeDB || options.URL != "bolt://127.0.0.1:7688" {
		t.Errorf("arcadedb = %+v", options)
	}
	if options.User != "root" || options.Password != "clm-arcadedb-root" || options.Database != "clm" {
		t.Errorf("arcadedb defaults = %+v", options)
	}
}

func TestTheBackendFlagBeatsTheEnvironment(t *testing.T) {
	unset(t)
	t.Setenv("SPECD_BOLT_BACKEND", "hydradb")
	options := resolve(t, "--bolt-backend", "arcadedb")
	if options.Backend != ArcadeDB || options.User != "root" {
		t.Errorf("the flag did not beat the environment: %+v", options)
	}
}

func TestTheSessionGraphFillsWhatNothingNamed(t *testing.T) {
	unset(t)
	SessionGraph = func() (Instance, bool) {
		return Instance{Backend: HydraDB, URL: "bolt://127.0.0.1:40001", User: "neo4j", PasswordFile: "/state/graph/token"}, true
	}
	t.Cleanup(func() { SessionGraph = nil })
	options := resolve(t)
	if options.URL != "bolt://127.0.0.1:40001" || options.PasswordFile != "/state/graph/token" || options.Backend != HydraDB {
		t.Errorf("session graph not taken: %+v", options)
	}
	flagged := resolve(t, "--bolt-url", "bolt://flag:1")
	if flagged.URL != "bolt://flag:1" || flagged.PasswordFile != "" {
		t.Errorf("a flag must beat the session graph: %+v", flagged)
	}
	t.Setenv("SPECD_BOLT_URL", "bolt://env:2")
	fromEnv := resolve(t)
	if fromEnv.URL != "bolt://env:2" {
		t.Errorf("the environment must beat the session graph: %+v", fromEnv)
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

func TestAnEmptyURLEnvironmentNamesNoGraph(t *testing.T) {
	unset(t)
	t.Setenv("SPECD_BOLT_URL", "")
	options := resolve(t)
	if options.URL != "" || options.Enabled() {
		t.Errorf("url = %q, want no graph named", options.URL)
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
	options := resolve(t, "--bolt-backend", "hydradb", "--bolt-password", "secret", "--bolt-url", "bolt://127.0.0.1:1")
	if options.Password != "secret" || options.PasswordFile != "" || options.URL != "bolt://127.0.0.1:1" {
		t.Errorf("options = %+v", options)
	}
}
