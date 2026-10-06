package gitrepo

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/orgfixture"
)

func TestGitlinksAreNotTrackedFiles(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("gitlinks", 0)
	ctx := context.Background()

	links, err := Gitlinks(ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, l := range links {
		paths = append(paths, l.Path)
		if len(l.Commit) != 40 {
			t.Fatalf("%s pins %q", l.Path, l.Commit)
		}
	}
	if want := []string{"hono-compute-provider", "market", "relay"}; !slices.Equal(paths, want) {
		t.Fatalf("gitlinks = %v, want %v", paths, want)
	}

	files, err := TrackedFiles(ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		for _, p := range paths {
			if file == p || strings.HasPrefix(file, p+"/") {
				t.Fatalf("tracked files hold %s: a submodule is a pointer, not a file", file)
			}
		}
	}
	for _, own := range []string{"CLAUDE.md", ".gitmodules", "docs/NOTES.md", "scripts/find-all-package.ts"} {
		if !slices.Contains(files, own) {
			t.Fatalf("the root's own file %s is missing from %v", own, files)
		}
	}

	excludes := GitlinkExcludes(ctx, clone)
	if want := []string{"hono-compute-provider/**", "market/**", "relay/**"}; !slices.Equal(excludes, want) {
		t.Fatalf("excludes = %v, want %v", excludes, want)
	}
}

func TestAPlainRepositoryHasNoGitlinks(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	seed := f.Seed("market")
	links, err := Gitlinks(context.Background(), seed)
	if err != nil || len(links) != 0 {
		t.Fatalf("links = %v %v", links, err)
	}
	if got := GitlinkExcludes(context.Background(), seed); len(got) != 0 {
		t.Fatalf("excludes = %v", got)
	}
}
