package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func TestPrintEffectsGroupsByContextAndFile(t *testing.T) {
	computed := []policy.Effect{
		{Kind: policy.EffectHTTPRequest, Component: "hono-bidder", File: "hono-bidder/mod.ts", Line: 12, Attrs: map[string]string{"url": "https://relay"}},
		{Kind: policy.EffectSSHConnect, Component: "hono-bidder", File: "hono-bidder/mod.ts", Line: 40, Attrs: map[string]string{"argv0": "ssh"}},
		{Kind: policy.EffectNetDial, Component: "lib", File: "lib/report.ts", Line: 9},
		{Kind: policy.EffectProcExec, File: "scripts/deploy.sh", Line: 4},
	}
	out := &bytes.Buffer{}
	printEffects(out, "market", "abcdef1234", computed)
	text := out.String()
	for _, want := range []string{
		"repository: market  commit: abcdef12",
		"effects: 4",
		"  http.request     1",
		"context (no context)",
		"context hono-bidder",
		"context lib",
		"  hono-bidder/mod.ts",
		"   12  http.request     url=https://relay",
		"   40  ssh.connect      argv0=ssh",
		"  lib/report.ts",
		"    9  net.dial",
		"  scripts/deploy.sh",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	contexts := []string{"context (no context)", "context hono-bidder", "context lib"}
	previous := -1
	for _, name := range contexts {
		index := strings.Index(text, name)
		if index < previous {
			t.Fatalf("contexts out of order:\n%s", text)
		}
		previous = index
	}
}

func TestEffectFilesKeepsSortedUnique(t *testing.T) {
	files := effectFiles([]policy.Effect{
		{File: "b.go"}, {File: "a.go"}, {File: "b.go"},
	})
	if strings.Join(files, ",") != "a.go,b.go" {
		t.Fatalf("unexpected files %v", files)
	}
}
