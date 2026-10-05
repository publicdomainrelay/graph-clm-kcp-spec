package policy

import (
	"testing"
)

func testGraph(source string, nodes []CodeGraphNode) CodeGraph {
	file := CodeGraphFile{Path: "svc.go", Language: "go", Context: "svc"}
	all := append([]CodeGraphNode{{
		ID: "file:svc.go", Kind: "file", Name: "svc.go", File: "svc.go",
		StartLine: 1, EndLine: 1 + countLines(source), Context: "svc", Text: source,
	}}, nodes...)
	return CodeGraph{
		APIVersion: APIVersion,
		Kind:       CodeGraphKind,
		Metadata:   ObjectMeta{Name: "svc"},
		Spec: CodeGraphSpec{
			Repository: "svc",
			Files:      []CodeGraphFile{file},
			Nodes:      all,
			Edges:      []CodeGraphEdge{},
			Texts:      map[string]string{"svc.go": source},
		},
	}
}

func countLines(source string) int {
	count := 0
	for _, char := range source {
		if char == '\n' {
			count++
		}
	}
	return count
}

func compileOne(t *testing.T, pack ClassifierPack) *Matcher {
	t.Helper()
	matcher, err := Compile(pack)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return matcher
}

func TestEffectsAttributeLineAndNode(t *testing.T) {
	source := "package svc\n\nfunc client() {\n\tnet.Dial(\"tcp\", \"guest.internal:22\")\n}\n"
	graph := testGraph(source, []CodeGraphNode{{
		ID: "fn:client", Kind: "function", Name: "client", File: "svc.go",
		StartLine: 3, EndLine: 5, Context: "svc",
	}})
	matcher := compileOne(t, ClassifierPack{
		Language: "go",
		Rules: []EffectRule{{
			ID: "net-dial", Kind: EffectNetDial,
			Call:   &CallRule{Name: "net.Dial", TargetArg: 1},
			Target: "target",
		}},
	})
	effects := matcher.Effects(graph, MatchOptions{})
	if len(effects) != 1 {
		t.Fatalf("got %d effects, want 1: %+v", len(effects), effects)
	}
	effect := effects[0]
	if effect.File != "svc.go" || effect.Line != 4 || effect.Node != "fn:client" {
		t.Fatalf("bad location: %+v", effect)
	}
	if effect.Context != "svc" || effect.Component != "svc" {
		t.Fatalf("bad context: %+v", effect)
	}
	if effect.Attr("target") != "guest.internal:22" {
		t.Fatalf("bad attrs: %+v", effect.Attrs)
	}
	if effect.ID != EffectID(EffectNetDial, "svc.go", 4, effect.Attrs) {
		t.Fatalf("unstable id: %s", effect.ID)
	}
}

func TestEffectsRouteNodeAndCallAgree(t *testing.T) {
	source := "package svc\n\nfunc routes(mux *http.ServeMux) {\n\tmux.HandleFunc(\"GET /tasks\", handler)\n}\n"
	graph := testGraph(source, []CodeGraphNode{
		{ID: "fn:routes", Kind: "function", Name: "routes", File: "svc.go", StartLine: 3, EndLine: 5, Context: "svc"},
		{ID: "route:x", Kind: "route", Name: "GET /tasks", File: "svc.go", StartLine: 4, EndLine: 4, Context: "svc"},
	})
	matcher := compileOne(t, ClassifierPack{
		Language: "go",
		Rules: []EffectRule{{
			ID: "handle", Kind: EffectHTTPHandle,
			Call: &CallRule{Name: "mux.HandleFunc"},
			Extract: []ExtractRule{
				{Attr: "method", Regex: `^\s*"([A-Z]+)\s`},
				{Attr: "path", Regex: `^\s*"(?:[A-Z]+\s+)?([^"\s]+)"`},
			},
		}, {
			ID: "route-node", Kind: EffectHTTPHandle, Route: &RouteRule{Node: true},
		}},
	})
	effects := matcher.Effects(graph, MatchOptions{})
	if len(effects) != 1 {
		t.Fatalf("call rule and route node must dedupe: %+v", effects)
	}
	if effects[0].Attr("method") != "GET" || effects[0].Attr("path") != "/tasks" {
		t.Fatalf("bad route attrs: %+v", effects[0].Attrs)
	}
}

func TestEffectsRouteNodeAlone(t *testing.T) {
	graph := testGraph("package svc\n", []CodeGraphNode{
		{ID: "route:y", Kind: "route", Name: "POST /entries", File: "svc.go", StartLine: 1, EndLine: 1, Context: "svc"},
	})
	matcher := compileOne(t, ClassifierPack{
		Language: "go",
		Rules:    []EffectRule{{ID: "route-node", Kind: EffectHTTPHandle, Route: &RouteRule{Node: true}}},
	})
	effects := matcher.Effects(graph, MatchOptions{})
	if len(effects) != 1 {
		t.Fatalf("got %d effects, want 1", len(effects))
	}
	if effects[0].Attr("method") != "POST" || effects[0].Attr("path") != "/entries" || effects[0].Node != "route:y" {
		t.Fatalf("bad route effect: %+v", effects[0])
	}
}

func TestEffectsImportMatcher(t *testing.T) {
	source := "package svc\n\nimport (\n\t\"os/exec\"\n)\n"
	graph := testGraph(source, nil)
	matcher := compileOne(t, ClassifierPack{
		Language: "go",
		Rules: []EffectRule{{
			ID: "exec-import", Kind: EffectProcExec,
			Import: &ImportRule{Specifier: `^os/exec$`},
		}},
	})
	effects := matcher.Effects(graph, MatchOptions{})
	if len(effects) != 1 {
		t.Fatalf("got %d effects, want 1: %+v", len(effects), effects)
	}
	if effects[0].Line != 4 || effects[0].Attr("specifier") != "os/exec" {
		t.Fatalf("bad import effect: %+v", effects[0])
	}
}

func TestEffectsRequiresImportGatesRule(t *testing.T) {
	pack := ClassifierPack{
		Language: "typescript",
		Rules: []EffectRule{{
			ID: "spawn", Kind: EffectProcExec,
			RequiresImport: `^node:child_process$`,
			Call:           &CallRule{Name: "spawn"},
		}},
	}
	matcher := compileOne(t, pack)
	without := matcher.EffectsIn("x.ts", "typescript", "spawn(\"ls\");\n", MatchOptions{})
	if len(without) != 0 {
		t.Fatalf("gated rule fired without the import: %+v", without)
	}
	with := matcher.EffectsIn("x.ts", "typescript", "import { spawn } from \"node:child_process\";\nspawn(\"ls\");\n", MatchOptions{})
	if len(with) != 1 || with[0].Line != 2 {
		t.Fatalf("gated rule did not fire: %+v", with)
	}
}

func TestEffectsFirstMatchWins(t *testing.T) {
	pack := ClassifierPack{
		Language: "typescript",
		Rules: []EffectRule{
			{ID: "ssh", Kind: EffectSSHConnect, Call: &CallRule{Name: "Deno.Command", Argv0: []string{"ssh"}}},
			{ID: "exec", Kind: EffectProcExec, Call: &CallRule{Name: "Deno.Command"}},
		},
	}
	matcher := compileOne(t, pack)
	effects := matcher.EffectsIn("x.ts", "typescript", "new Deno.Command(\"ssh\", {});\nnew Deno.Command(\"git\", {});\n", MatchOptions{})
	if len(effects) != 2 {
		t.Fatalf("got %d effects, want 2: %+v", len(effects), effects)
	}
	if effects[0].Kind != EffectSSHConnect || effects[1].Kind != EffectProcExec {
		t.Fatalf("first match did not win: %+v", effects)
	}
}

func TestEffectsCommandVerbs(t *testing.T) {
	pack := ClassifierPack{
		Language: "shell",
		Rules: []EffectRule{{
			ID: "container", Kind: EffectContainerExec,
			Command: &CommandRule{Name: "docker|podman", Verbs: []string{"exec", "run"}},
		}},
	}
	matcher := compileOne(t, pack)
	effects := matcher.EffectsIn("deploy.sh", "shell", "docker exec -i guest sh\ndocker ps\npodman run --rm alpine\n", MatchOptions{})
	if len(effects) != 2 {
		t.Fatalf("got %d effects, want 2: %+v", len(effects), effects)
	}
	if effects[0].Attr("argv0") != "docker" || effects[1].Attr("argv0") != "podman" {
		t.Fatalf("bad argv0: %+v", effects)
	}
}

func TestEffectsIgnoreCommentsAndStrings(t *testing.T) {
	source := "package svc\n\n// net.Dial(\"tcp\", \"x\")\nvar decoy = \"net.Dial(\\\"tcp\\\", \\\"y\\\")\"\nnet.Dial(\"tcp\", \"z:1\")\n"
	matcher := compileOne(t, ClassifierPack{
		Language: "go",
		Rules: []EffectRule{{
			ID: "net-dial", Kind: EffectNetDial,
			Call: &CallRule{Name: "net.Dial", TargetArg: 1}, Target: "target",
		}},
	})
	effects := matcher.EffectsIn("svc.go", "go", source, MatchOptions{})
	if len(effects) != 1 || effects[0].Line != 5 {
		t.Fatalf("comment or string matched: %+v", effects)
	}
}

func TestEffectsAttributeStayAtTheCallSite(t *testing.T) {
	source := "import { createRelay } from \"x\";\n\nDeno.test(\"direct\", () => {\n  new Deno.Command(\"ssh\", { args: [\"-p\", \"2222\", \"root@10.0.0.7\", \"true\"] });\n});\n\nDeno.test(\"relayed\", () => {\n  new Deno.Command(\"ssh\", { args: [\"-o\", \"ProxyCommand=websocat --binary ws://relay/x\", \"root@guest\"] });\n});\n"
	graph := testGraph(source, nil)
	graph.Spec.Files[0].Language = "typescript"
	matcher := compileOne(t, ClassifierPack{
		Language: "typescript",
		Rules: []EffectRule{{
			ID: "ssh", Kind: EffectSSHConnect,
			Call: &CallRule{Name: "Deno.Command", Argv0: []string{"ssh"}},
			Extract: []ExtractRule{
				{Attr: "proxyCommand", Regex: `ProxyCommand=([^\n,;]+)`, Trim: true},
			},
		}},
	})
	effects := matcher.Effects(graph, MatchOptions{})
	if len(effects) != 2 {
		t.Fatalf("got %d effects, want 2: %+v", len(effects), effects)
	}
	for _, effect := range effects {
		switch effect.Line {
		case 4:
			if effect.Attr("proxyCommand") != "" {
				t.Fatalf("direct ssh lent the other call's ProxyCommand: %+v", effect.Attrs)
			}
		case 8:
			if effect.Attr("proxyCommand") == "" {
				t.Fatalf("relayed ssh lost its own ProxyCommand: %+v", effect.Attrs)
			}
		default:
			t.Fatalf("unexpected ssh site: %+v", effect)
		}
	}
}

func TestEffectsResolveConstantAliases(t *testing.T) {
	pack := ClassifierPack{
		Language: "typescript",
		Rules: []EffectRule{
			{
				ID: "ssh", Kind: EffectSSHConnect,
				Call:    &CallRule{Name: "Deno.Command", Argv0: []string{"ssh"}},
				Target:  "argv0",
				Extract: []ExtractRule{{Attr: "proxyCommand", Regex: `ProxyCommand=([^\n,;]+)`, Trim: true}},
			},
			{
				ID: "dial", Kind: EffectNetDial,
				Call:    &CallRule{Name: "Deno.connect"},
				Extract: []ExtractRule{{Attr: "target", Regex: `hostname\s*:\s*["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`}},
			},
		},
	}
	matcher := compileOne(t, pack)
	source := "const SSH = \"ssh\";\nconst host = \"10.0.0.7\";\nconst proxy = \"ProxyCommand=websocat --binary ws://relay/x\";\nnew Deno.Command(SSH, { args: [\"-o\", proxy, `root@${host}`] });\nawait Deno.connect({ hostname: host, port: 2222 });\n"
	effects := matcher.EffectsIn("x.ts", "typescript", source, MatchOptions{})
	if len(effects) != 2 {
		t.Fatalf("got %d effects, want 2: %+v", len(effects), effects)
	}
	if effects[0].Kind != EffectSSHConnect || effects[0].Attr("proxyCommand") == "" || effects[0].Attr("argv0") != "ssh" {
		t.Fatalf("alias argv0 or proxy not resolved: %+v", effects[0])
	}
	if effects[1].Kind != EffectNetDial || effects[1].Attr("target") != "10.0.0.7" {
		t.Fatalf("host variable not resolved: %+v", effects[1])
	}
}

func TestEffectsResolveImportedConstants(t *testing.T) {
	common := "export const EVENT_NSID = \"com.example.events.onNetwork\";\n"
	bidder := "import { EVENT_NSID } from \"@market/market-common\";\n\nexport function emit() {\n  repo.createSignedRepoRecord(EVENT_NSID, {});\n}\n"
	graph := CodeGraph{
		APIVersion: APIVersion,
		Kind:       CodeGraphKind,
		Metadata:   ObjectMeta{Name: "sample"},
		Spec: CodeGraphSpec{
			Repository: "sample",
			Files: []CodeGraphFile{
				{Path: "lib/market-common/mod.ts", Language: "typescript"},
				{Path: "lib/bidder/mod.ts", Language: "typescript"},
			},
			Nodes: []CodeGraphNode{
				{ID: "fn:emit", Kind: "function", Name: "emit", File: "lib/bidder/mod.ts", StartLine: 3, EndLine: 5, Text: bidder},
			},
			Edges: []CodeGraphEdge{},
			Texts: map[string]string{"lib/market-common/mod.ts": common, "lib/bidder/mod.ts": bidder},
		},
	}
	matcher := compileOne(t, ClassifierPack{
		Language: "typescript",
		Rules: []EffectRule{{
			ID: "emit", Kind: EffectEventEmit, Extra: true,
			Call: &CallRule{Name: "createSignedRepoRecord", Any: true},
			Extract: []ExtractRule{
				{Attr: "type", Regex: `^\s*["\x27\x60]?([A-Za-z_$][\w$.]*)`},
			},
		}},
	})
	effects := matcher.Effects(graph, MatchOptions{IncludeExtras: true})
	if len(effects) != 1 {
		t.Fatalf("got %d effects, want 1: %+v", len(effects), effects)
	}
	if effects[0].Attr("type") != "com.example.events.onNetwork" {
		t.Fatalf("imported NSID not resolved: %+v", effects[0].Attrs)
	}
}

func TestEffectsConstantAliasesStayInCode(t *testing.T) {
	pack := ClassifierPack{
		Language: "go",
		Rules: []EffectRule{{
			ID: "ssh", Kind: EffectSSHConnect,
			Call: &CallRule{Name: "exec.Command", Argv0: []string{"ssh"}},
		}},
	}
	matcher := compileOne(t, pack)
	source := "package svc\n\nconst sshBin = \"ssh\"\n\nvar note = \"sshBin\"\n\nfunc run() {\n\tother.sshBin(\"x\")\n\texec.Command(sshBin, \"root@guest\")\n}\n"
	effects := matcher.EffectsIn("svc.go", "go", source, MatchOptions{})
	if len(effects) != 1 || effects[0].Line != 9 {
		t.Fatalf("constant alias matched outside its call: %+v", effects)
	}
}

func TestEffectsExtrasToggle(t *testing.T) {
	pack := ClassifierPack{
		Language: "typescript",
		Rules: []EffectRule{{
			ID: "rpc", Kind: EffectHTTPRequest, Extra: true,
			Call: &CallRule{Name: "callService", Member: true},
		}},
	}
	matcher := compileOne(t, pack)
	if got := matcher.EffectsIn("x.ts", "typescript", "agent.callService(url);\n", MatchOptions{}); len(got) != 0 {
		t.Fatalf("extra fired when disabled: %+v", got)
	}
	got := matcher.EffectsIn("x.ts", "typescript", "agent.callService(url);\n", MatchOptions{IncludeExtras: true})
	if len(got) != 1 {
		t.Fatalf("extra did not fire when enabled: %+v", got)
	}
}

func TestEffectsRejectUnknownKind(t *testing.T) {
	_, err := Compile(ClassifierPack{
		Language: "go",
		Rules:    []EffectRule{{ID: "bad", Kind: "net.teleport"}},
	})
	if err == nil {
		t.Fatal("unknown effect kind compiled")
	}
}

func TestEffectIDIsStableAndAttrOrdered(t *testing.T) {
	first := EffectID(EffectNetDial, "a.go", 1, map[string]string{"a": "1", "b": "2"})
	second := EffectID(EffectNetDial, "a.go", 1, map[string]string{"b": "2", "a": "1"})
	if first != second {
		t.Fatalf("id depends on map order: %s != %s", first, second)
	}
	if first == EffectID(EffectNetDial, "a.go", 2, map[string]string{"a": "1", "b": "2"}) {
		t.Fatal("id does not depend on line")
	}
}
