package spec

import (
	"reflect"
	"testing"
)

func TestAbsoluteMachinePath(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"The manifests live under /home/johnandersen777/src/publicdomainrelay-kcp.", "/home/johnandersen777"},
		{"reads the token from /tmp/hdb/token", "/tmp/hdb"},
		{"The file deploy/examples/atproto/market/apply.sh names the siblings.", ""},
		{"The pod mounts /var/run/secrets/kubernetes.io/serviceaccount.", ""},
		{"A URL like https://example.com/docs is fine.", ""},
	}
	for _, entry := range cases {
		got, found := AbsoluteMachinePath(entry.text)
		if entry.want == "" {
			if found {
				t.Errorf("%q named the machine path %q", entry.text, got)
			}
			continue
		}
		if !found || got != entry.want {
			t.Errorf("AbsoluteMachinePath(%q) = %q/%v, want %q", entry.text, got, found, entry.want)
		}
	}
}

func TestCommandContext(t *testing.T) {
	if !CommandContext([]string{"cmd/deno-kcp-provider/main.go"}) {
		t.Error("cmd/deno-kcp-provider/main.go is a command entrypoint")
	}
	if !CommandContext([]string{"internal/baoembed/cmd/baoembed/main.go"}) {
		t.Error("a nested cmd/ directory is a command entrypoint")
	}
	if CommandContext([]string{"internal/provider/watch.go"}) {
		t.Error("internal/provider is not a command entrypoint")
	}
}

func TestDeclaresConfigSurface(t *testing.T) {
	withFlags := []Requirement{{ID: "r.config", Level: LevelMust, Text: "main reads --kubeconfig (KUBECONFIG) and --runs-dir (RUNS_DIR, default runs)."}}
	if !DeclaresConfigSurface(withFlags) {
		t.Error("a requirement naming flags and their environment variables declares the surface")
	}
	withEnvOnly := []Requirement{{ID: "r.env", Level: LevelMust, Text: "main reads OPENBAO_ADDR and OPENBAO_TOKEN from the environment."}}
	if !DeclaresConfigSurface(withEnvOnly) {
		t.Error("a requirement naming environment variables declares the surface")
	}
	without := []Requirement{{ID: "r.wiring", Level: LevelMust, Text: "main builds the registry and then the provider."}}
	if DeclaresConfigSurface(without) {
		t.Error("a requirement about wiring declares no surface")
	}
}

func TestEnumeratesNames(t *testing.T) {
	if !EnumeratesNames("Get, List, Watch, Create, Update, Delete") {
		t.Error("a list of method names is a bare enumeration")
	}
	if EnumeratesNames("main sets KUBE_FEATURE_WatchListClient to false, because the virtual workspace sends no bookmark.") {
		t.Error("a sentence is not an enumeration")
	}
	if !EnumeratesNames("read, write, run") {
		t.Error("three bare words are as empty as three method names")
	}
}

func TestNamedFlagsAndEnvironments(t *testing.T) {
	text := "runs with --kubeconfig and --runs-dir, reading KUBECONFIG and RUNS_DIR; MUST hold."
	if got := NamedFlags(text); !reflect.DeepEqual(got, []string{"--kubeconfig", "--runs-dir"}) {
		t.Errorf("flags = %v", got)
	}
	if got := NamedEnvironments(text); !reflect.DeepEqual(got, []string{"KUBECONFIG", "RUNS_DIR"}) {
		t.Errorf("environments = %v", got)
	}
}
