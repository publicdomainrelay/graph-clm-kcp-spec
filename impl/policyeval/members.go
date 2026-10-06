package policyeval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

const MembersDir = "policy-members"

type MemberOptions struct {
	CacheDir string

	WorkDir string

	Lock *policy.PackLock

	Verify bool

	Paths map[string]string
}

type ResolvedMember struct {
	Member policy.Member

	Root string

	Dir string

	Commit string

	ClassifiersDir string

	cleanup func()
}

func (m ResolvedMember) Cleanup() {
	if m.cleanup != nil {
		m.cleanup()
	}
}

func (m ResolvedMember) Lock() policy.MemberLock {
	return policy.MemberLock{Name: m.Member.Name, URL: m.Member.URL, Ref: m.Member.Ref, Commit: m.Commit}
}

func (m ResolvedMember) Report() policy.ReportMember {
	return policy.ReportMember{Name: m.Member.Name, URL: m.Member.URL, Ref: m.Member.Ref, Commit: m.Commit}
}

func ResolveMembers(ctx context.Context, members []policy.Member, library policy.Library, opts MemberOptions) ([]ResolvedMember, error) {
	out := make([]ResolvedMember, 0, len(members))
	for _, member := range members {
		resolved, err := resolveMember(ctx, member, library, opts)
		if err != nil {
			for _, done := range out {
				done.Cleanup()
			}
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

func resolveMember(ctx context.Context, member policy.Member, library policy.Library, opts MemberOptions) (ResolvedMember, error) {
	if member.Name == "" {
		return ResolvedMember{}, fmt.Errorf("policyeval: a member names no name")
	}
	if member.URL == "" {
		return ResolvedMember{}, fmt.Errorf("policyeval: member %s names no url", member.Name)
	}
	root, commit, cleanup, err := memberCheckout(ctx, member, opts)
	if err != nil {
		return ResolvedMember{}, err
	}
	dir := root
	if member.Path != "" {
		dir = filepath.Join(root, filepath.FromSlash(member.Path))
		if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
			cleanup()
			return ResolvedMember{}, fmt.Errorf("policyeval: member %s has no %s", member.Name, member.Path)
		}
	}
	classifiers, classifiersCleanup, err := memberClassifiers(member, library, dir, opts)
	if err != nil {
		cleanup()
		return ResolvedMember{}, err
	}
	pin := policy.MemberLock{Name: member.Name, URL: member.URL, Ref: member.Ref, Commit: commit}
	if opts.Verify && opts.Lock != nil && !opts.Lock.MemberMatches(pin) {
		pinned, ok := opts.Lock.Member(member.Name)
		classifiersCleanup()
		cleanup()
		if !ok {
			return ResolvedMember{}, fmt.Errorf("policyeval: member %s is not in %s; run specctl policy build --relock",
				member.Name, policy.LockPath)
		}
		return ResolvedMember{}, fmt.Errorf("policyeval: member %s resolves to %s, %s pins %s; move the ref or run specctl policy build --relock",
			member.Name, commit, policy.LockPath, pinned.Commit)
	}
	return ResolvedMember{
		Member:         member,
		Root:           root,
		Dir:            dir,
		Commit:         commit,
		ClassifiersDir: classifiers,
		cleanup: func() {
			classifiersCleanup()
			cleanup()
		},
	}, nil
}

func memberCheckout(ctx context.Context, member policy.Member, opts MemberOptions) (string, string, func(), error) {
	removeCache := false
	cache := opts.CacheDir
	if cache == "" {
		temp, err := os.MkdirTemp(opts.WorkDir, "specd-members-")
		if err != nil {
			return "", "", func() {}, err
		}
		cache = temp
		removeCache = true
	}
	cleanup := func() {
		if removeCache {
			os.RemoveAll(cache)
		}
	}
	dir := filepath.Join(cache, MembersDir, member.Name)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			cleanup()
			return "", "", func() {}, err
		}
		if out, err := runGit(ctx, "", "clone", "--quiet", "--", cloneURL(member, opts), dir); err != nil {
			cleanup()
			return "", "", func() {}, fmt.Errorf("policyeval: clone member %s: %v: %s", member.Name, err, strings.TrimSpace(out))
		}
	}
	if _, err := runGit(ctx, dir, "fetch", "--quiet", "--tags", "origin"); err != nil {
		if _, revErr := memberRevision(ctx, dir, member.Ref); revErr != nil {
			cleanup()
			return "", "", func() {}, fmt.Errorf("policyeval: fetch member %s: %v", member.Name, err)
		}
	}
	revision, err := memberRevision(ctx, dir, member.Ref)
	if err != nil {
		cleanup()
		return "", "", func() {}, fmt.Errorf("policyeval: member %s: %w", member.Name, err)
	}
	if out, err := runGit(ctx, dir, "checkout", "--quiet", "--force", "--detach", revision); err != nil {
		cleanup()
		return "", "", func() {}, fmt.Errorf("policyeval: member %s checkout %s: %v: %s", member.Name, member.Ref, err, strings.TrimSpace(out))
	}
	commit, err := memberRevision(ctx, dir, "HEAD")
	if err != nil {
		cleanup()
		return "", "", func() {}, fmt.Errorf("policyeval: member %s: %w", member.Name, err)
	}
	return dir, commit, cleanup, nil
}

func memberRevision(ctx context.Context, dir, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	for _, candidate := range []string{ref, "origin/" + ref, "refs/tags/" + ref, "FETCH_HEAD"} {
		out, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", candidate+"^{commit}")
		if err == nil {
			return strings.TrimSpace(out), nil
		}
	}
	return "", fmt.Errorf("ref %s resolves to no commit", ref)
}

func memberClassifiers(member policy.Member, library policy.Library, dir string, opts MemberOptions) (string, func(), error) {
	return namedClassifiers(member.Classifiers, library, dir, opts.WorkDir, "member "+member.Name)
}

func LibraryClassifierDirs(library policy.Library, dir, workDir string) ([]string, func(), error) {
	named, cleanup, err := namedClassifiers(library.Manifest.Classifiers, library, dir, workDir, "library "+library.Manifest.Repository)
	if err != nil || named == "" {
		return nil, cleanup, err
	}
	return []string{named}, cleanup, nil
}

func namedClassifiers(names []string, library policy.Library, dir, workDir, owner string) (string, func(), error) {
	if len(names) == 0 {
		candidate := filepath.Join(dir, policy.ClassifiersDir)
		if _, err := os.Stat(candidate); err != nil {
			return "", func() {}, nil
		}
		return candidate, func() {}, nil
	}
	temp, err := os.MkdirTemp(workDir, "specd-classifiers-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(temp) }
	for _, name := range names {
		source := policy.ClassifiersDir + "/" + name
		data, ok := library.Files[source]
		if !ok {
			cleanup()
			return "", func() {}, fmt.Errorf("policyeval: %s names the classifier %s, the library has no %s",
				owner, name, source)
		}
		if err := os.WriteFile(filepath.Join(temp, name), data, 0o644); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return temp, cleanup, nil
}

func cloneURL(member policy.Member, opts MemberOptions) string {
	if path, ok := opts.Paths[member.Name]; ok && path != "" {
		return path
	}
	return member.URL
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		command.Dir = dir
	}
	out, err := command.CombinedOutput()
	return string(out), err
}
