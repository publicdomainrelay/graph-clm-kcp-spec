package effects

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(data)
}

func match(t *testing.T, language, source string, extras bool) []policy.Effect {
	t.Helper()
	packs, err := Embedded()
	if err != nil {
		t.Fatalf("embedded packs: %v", err)
	}
	matcher, err := policy.Compile(packs...)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return matcher.EffectsIn("fixture", language, source, policy.MatchOptions{IncludeExtras: extras})
}

func summary(effects []policy.Effect) []string {
	out := make([]string, 0, len(effects))
	for _, effect := range effects {
		out = append(out, fmt.Sprintf("%d|%s|%s", effect.Line, effect.Kind, signature(effect.Attrs)))
	}
	return out
}

func signature(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, key := range keys {
		parts = append(parts, key+"="+attrs[key])
	}
	return strings.Join(parts, " ")
}

func assertEffects(t *testing.T, got []policy.Effect, want []string) {
	t.Helper()
	have := summary(got)
	if strings.Join(have, "\n") == strings.Join(want, "\n") {
		return
	}
	t.Fatalf("effects mismatch\n got:\n%s\nwant:\n%s", strings.Join(have, "\n"), strings.Join(want, "\n"))
}

func TestTypeScriptFixture(t *testing.T) {
	effects := match(t, "typescript", fixture(t, "typescript/service.ts"), true)
	assertEffects(t, effects, []string{
		"10|http.handle|method=GET path=/health",
		"11|http.handle|method=POST path=/xrpc/com.example.market.evaluate",
		"12|http.handle|method=ROUTE path=/",
		"16|http.request|",
		"17|http.request|",
		"18|event.emit|type=com.example.record",
		"19|event.emit|type=com.example.record",
		"20|event.receive|",
		"25|ssh.connect|argv0=ssh proxyCommand=websocat --binary wss://relay.example/tunnel",
		"35|container.exec|runtime=docker verb=exec",
		"36|proc.exec|argv0=ssh-keygen",
		"41|net.listen|host=127.0.0.1 port=8080",
		"42|net.listen|port=9999",
		"43|net.dial|port=22 target=10.0.0.5",
		"44|net.dial|url=wss://relay.example/tunnel",
		"51|http.request|argv0=curl",
		"57|container.exec|runtime=compute-provider verb=getNodeId",
	})
}

func TestTypeScriptWithoutExtras(t *testing.T) {
	effects := match(t, "typescript", fixture(t, "typescript/service.ts"), false)
	for _, effect := range effects {
		switch effect.Kind {
		case policy.EffectEventEmit, policy.EffectEventReceive:
			t.Fatalf("extra effect %s survived IncludeExtras=false", effect.Kind)
		}
	}
	bytes := 0
	for _, effect := range effects {
		if effect.Line == 17 {
			t.Fatalf("callService counted as http.request without extras")
		}
		bytes++
	}
	if bytes != 12 {
		t.Fatalf("got %d effects without extras, want 12", bytes)
	}
}

func TestGoFixture(t *testing.T) {
	effects := match(t, "go", fixture(t, "go/service.go"), true)
	assertEffects(t, effects, []string{
		"16|net.dial|target=guest.internal:22",
		"21|http.request|url=https://relay.example/status",
		"26|ssh.connect|",
		"34|container.exec|runtime=docker",
		"36|proc.exec|argv0=ssh-keygen",
		"42|http.handle|method=GET path=/accounts/{id}/balance",
		"43|http.handle|method=POST path=/entries",
		"44|net.listen|target=127.0.0.1:8080",
		"52|file.write|",
		"53|file.write|",
	})
}

func TestShellFixture(t *testing.T) {
	effects := match(t, "shell", fixture(t, "shell/deploy.sh"), true)
	assertEffects(t, effects, []string{
		"7|http.request|argv0=curl url=https://relay.example/status",
		"8|http.request|argv0=wget url=https://relay.example/health",
		"10|ssh.connect|argv0=ssh host=root@guest.internal proxyCommand=websocat",
		"11|proc.exec|argv0=ssh-keygen",
		"12|ssh.connect|argv0=scp host=root@guest.internal",
		"14|container.exec|argv0=docker runtime=docker verb=exec",
		"15|container.exec|argv0=container runtime=container verb=run",
		"16|container.exec|argv0=podman runtime=podman verb=inspect",
		"18|net.dial|argv0=nc",
		"19|net.dial|argv0=socat",
		"21|file.write|argv0=tee",
	})
}

func TestFixturesAreQuietInCommentsAndStrings(t *testing.T) {
	effects := match(t, "typescript", fixture(t, "typescript/service.ts"), true)
	for _, effect := range effects {
		if effect.Line == 3 || effect.Line == 5 || effect.Line == 7 {
			t.Fatalf("decoy on line %d classified: %+v", effect.Line, effect)
		}
	}
	goEffects := match(t, "go", fixture(t, "go/service.go"), true)
	for _, effect := range goEffects {
		if effect.Line == 11 || effect.Line == 13 {
			t.Fatalf("decoy on line %d classified: %+v", effect.Line, effect)
		}
	}
}

func TestPacksLoadFromDirectory(t *testing.T) {
	dir := t.TempDir()
	extra := `language: typescript
version: "1"
rules:
  - id: custom-rpc
    kind: http.request
    call: {name: callBidder, member: true}
`
	if err := os.WriteFile(filepath.Join(dir, "market.yaml"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	packs, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(packs) != 1 || packs[0].Language != "typescript" {
		t.Fatalf("unexpected packs %+v", packs)
	}
	matcher, err := policy.Compile(packs...)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	effects := matcher.EffectsIn("x.ts", "typescript", "await agent.callBidder(url);\n", policy.MatchOptions{})
	if len(effects) != 1 || effects[0].Kind != policy.EffectHTTPRequest {
		t.Fatalf("unexpected effects %+v", effects)
	}
}

func TestApplyWritesEffectsOntoGraph(t *testing.T) {
	graph := policy.CodeGraph{
		APIVersion: policy.APIVersion,
		Kind:       policy.CodeGraphKind,
		Metadata:   policy.ObjectMeta{Name: "fixture"},
		Spec: policy.CodeGraphSpec{
			Repository: "fixture",
			Files:      []policy.CodeGraphFile{{Path: "deploy.sh", Language: "shell"}},
			Texts:      map[string]string{"deploy.sh": "curl https://relay.example/status\n"},
		},
	}
	computed, err := Apply(&graph, Options{IncludeExtras: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(computed) != 1 {
		t.Fatalf("got %d effects, want 1", len(computed))
	}
	if len(graph.Spec.Effects) != 1 || graph.Spec.Effects[0].File != "deploy.sh" {
		t.Fatalf("graph effects not written: %+v", graph.Spec.Effects)
	}
}

func TestDirsNamesTheWorktreePacksOnlyWhenPresent(t *testing.T) {
	if got := Dirs(""); got != nil {
		t.Errorf("Dirs(\"\") = %v", got)
	}
	empty := t.TempDir()
	if got := Dirs(empty); got != nil {
		t.Errorf("Dirs of a worktree without classifiers/ = %v", got)
	}
	withPacks := t.TempDir()
	dir := filepath.Join(withPacks, ClassifiersDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := Dirs(withPacks)
	if len(got) != 1 || got[0] != dir {
		t.Errorf("Dirs = %v, want [%s]", got, dir)
	}
	packs, err := Packs(Options{ClassifiersDirs: got})
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) == 0 {
		t.Error("the embedded packs are missing")
	}
}
