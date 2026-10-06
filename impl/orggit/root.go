package orggit

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"sigs.k8s.io/yaml"
)

// Root is an org root: a git superproject whose submodules are the members.
type Root struct {
	// Dir is the top level of the superproject.
	Dir string

	// Name is the repository name the root's own branches use. It defaults to
	// the directory name, the way ingest names a repository.
	Name string

	// Env is extra environment for every git call (identity, protocol.file).
	Env []string
}

// Open finds the superproject that contains dir.
func Open(ctx context.Context, dir string) (*Root, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := &Root{}
	top, err := root.run(ctx, absolute, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("orggit: %s is not in a git checkout: %w", dir, err)
	}
	// A submodule checkout is a checkout too: climb to its superproject so a
	// command run inside a member still sees the whole org.
	for {
		super, err := root.run(ctx, top, "rev-parse", "--show-superproject-working-tree")
		if err != nil || super == "" {
			break
		}
		top = super
	}
	root.Dir = top
	root.Name = filepath.Base(top)
	return root, nil
}

// Store is the plumbing view of the root's own branches.
func (r *Root) Store() oagit.Store { return oagit.Store{Repo: r.Dir} }

// CurrentBranch of the root, empty when detached.
func (r *Root) CurrentBranch(ctx context.Context) string { return r.Store().CurrentBranch(ctx) }

// ArchBranch is the architecture branch of the root for its checked-out code
// branch, named the way every other repository's is.
func (r *Root) ArchBranch(ctx context.Context) string {
	store := r.Store()
	return oabranch.BranchFor(r.Name, store.CurrentBranch(ctx), store.DefaultBranch(ctx))
}

// PolicyBranch is the policy branch of the root.
func (r *Root) PolicyBranch(ctx context.Context) string {
	store := r.Store()
	return policy.Resolve(r.Name, store.CurrentBranch(ctx), store.DefaultBranch(ctx))
}

// HasSubmodules reports whether the checkout has any gitlink: the test for
// "this repository is an org root".
func (r *Root) HasSubmodules(ctx context.Context) bool {
	links, err := r.gitlinks(ctx, "")
	return err == nil && len(links) > 0
}

type link struct {
	path   string
	commit string
}

// gitlinks reads the pinned commits: from the index when ref is empty (what the
// next commit records), from a root commit otherwise.
func (r *Root) gitlinks(ctx context.Context, ref string) ([]link, error) {
	var raw string
	var err error
	if ref == "" {
		raw, err = r.output(ctx, r.Dir, "ls-files", "--stage", "-z")
	} else {
		raw, err = r.output(ctx, r.Dir, "ls-tree", "-r", "-z", "--full-tree", ref)
	}
	if err != nil {
		return nil, err
	}
	var out []link
	for _, record := range strings.Split(raw, "\x00") {
		meta, file, found := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !found {
			continue
		}
		if ref == "" && len(fields) >= 3 && fields[0] == org.GitlinkMode {
			out = append(out, link{path: file, commit: fields[1]})
		}
		if ref != "" && len(fields) >= 3 && fields[0] == org.GitlinkMode {
			out = append(out, link{path: file, commit: fields[2]})
		}
	}
	return out, nil
}

func (r *Root) gitmodules(ctx context.Context, ref string) ([]org.Submodule, error) {
	var text string
	if ref == "" {
		data, err := os.ReadFile(filepath.Join(r.Dir, ".gitmodules"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		text = string(data)
	} else {
		out, err := r.output(ctx, r.Dir, "show", ref+":.gitmodules")
		if err != nil {
			text = ""
		} else {
			text = out
		}
	}
	return org.ParseGitmodules(text)
}

// MembersOptions selects what Members reads.
type MembersOptions struct {
	// Ref is a root commit; empty means the index, the state the next commit
	// would record.
	Ref string
}

// Members lists every submodule with its pin and the state of its checkout.
// Nothing here writes anywhere.
func (r *Root) Members(ctx context.Context, options MembersOptions) ([]org.MemberState, error) {
	links, err := r.gitlinks(ctx, options.Ref)
	if err != nil {
		return nil, err
	}
	subs, err := r.gitmodules(ctx, options.Ref)
	if err != nil {
		return nil, err
	}
	byPath := map[string]org.Submodule{}
	for _, sub := range subs {
		byPath[sub.Path] = sub
	}
	out := make([]org.MemberState, 0, len(links))
	for _, l := range links {
		sub, ok := byPath[l.path]
		if !ok {
			sub = org.Submodule{Name: l.path, Path: l.path}
		}
		state, err := r.memberState(ctx, sub, l.commit)
		if err != nil {
			return nil, err
		}
		out = append(out, state)
	}
	sortStates(out)
	return out, nil
}

func sortStates(states []org.MemberState) {
	for i := 1; i < len(states); i++ {
		for j := i; j > 0 && states[j].Path < states[j-1].Path; j-- {
			states[j], states[j-1] = states[j-1], states[j]
		}
	}
}

// MemberDir is the checkout directory of a member.
func (r *Root) MemberDir(member org.Member) string {
	return filepath.Join(r.Dir, filepath.FromSlash(member.Path))
}

func (r *Root) initialized(dir string) bool {
	// An uninitialized submodule is an empty directory inside the superproject:
	// git would answer for the superproject, so the test is the member's own
	// .git entry.
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func (r *Root) memberState(ctx context.Context, sub org.Submodule, pinned string) (org.MemberState, error) {
	state := org.MemberState{
		Member: org.Member{
			Name:       path.Base(sub.Path),
			Path:       sub.Path,
			URL:        sub.URL,
			Branch:     sub.Branch,
			CodeCommit: pinned,
			Candidates: sub.RepositoryCandidates(),
		},
	}
	dir := filepath.Join(r.Dir, filepath.FromSlash(sub.Path))
	state.Initialized = r.initialized(dir)
	if !state.Initialized {
		state.State = org.StateUninitialized
		return state, nil
	}
	state.Head, _ = r.run(ctx, dir, "rev-parse", "HEAD")
	state.Branch, _ = r.run(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if status, err := r.run(ctx, dir, "status", "--porcelain", "--untracked-files=no"); err == nil {
		state.Dirty = status != ""
	}
	state.PinPresent = r.hasCommit(ctx, dir, pinned)
	if state.PinPresent {
		if state.Head != "" && state.Head != pinned {
			state.Ahead = r.count(ctx, dir, pinned+".."+state.Head)
			state.Behind = r.count(ctx, dir, state.Head+".."+pinned)
		}
		contains, err := r.run(ctx, dir, "branch", "-r", "--contains", pinned)
		state.Published = err == nil && contains != ""
	}
	if err := r.resolveSpec(ctx, &state, dir); err != nil {
		return state, err
	}
	return state, nil
}

func (r *Root) hasCommit(ctx context.Context, dir, commit string) bool {
	if commit == "" {
		return false
	}
	_, err := r.run(ctx, dir, "cat-file", "-e", commit+"^{commit}")
	return err == nil
}

func (r *Root) count(ctx context.Context, dir, rangeSpec string) int {
	out, err := r.run(ctx, dir, "rev-list", "--count", rangeSpec)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(out)
	return n
}

// branchTip finds an orphan branch of a member: a local branch first, then the
// remote-tracking one a clone leaves. It returns the ref that resolved.
func (r *Root) branchTip(ctx context.Context, dir, branch string) (ref, commit string) {
	for _, candidate := range []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch} {
		if out, err := r.run(ctx, dir, "rev-parse", "--verify", "--quiet", candidate+"^{commit}"); err == nil && out != "" {
			return candidate, out
		}
	}
	return "", ""
}

// resolveSpec finds the member's architecture and policy branches and the
// architecture commit that describes the pinned code.
func (r *Root) resolveSpec(ctx context.Context, state *org.MemberState, dir string) error {
	for _, name := range state.Candidates {
		archBranch := oabranch.Branch(name)
		ref, tip := r.branchTip(ctx, dir, archBranch)
		if ref == "" {
			continue
		}
		state.Name = name
		state.Arch = &org.BranchRef{Branch: archBranch, Commit: tip}
		history, err := r.archHistory(ctx, dir, ref)
		if err != nil {
			return err
		}
		isAncestor := func(ancestor, commit string) bool {
			_, err := r.run(ctx, dir, "merge-base", "--is-ancestor", ancestor, commit)
			return err == nil
		}
		entry, selected := org.SelectArch(history, state.CodeCommit, isAncestor)
		if selected {
			state.Arch.Commit = entry.Commit
			state.Arch.IndexedCommit = entry.IndexedCommit
		}
		state.State = org.StateOf(true, true, selected)
		break
	}
	if state.Arch == nil {
		state.State = org.StateOf(true, false, false)
	}
	for _, name := range append([]string{state.Name}, state.Candidates...) {
		branch := policy.Branch(name)
		if ref, tip := r.branchTip(ctx, dir, branch); ref != "" {
			state.Policy = &org.BranchRef{Branch: branch, Commit: tip}
			break
		}
	}
	return nil
}

const maxArchHistory = 400

// archHistory lists the architecture commits of a member newest first, each
// with the code commit its repository.yaml records.
func (r *Root) archHistory(ctx context.Context, dir, ref string) ([]org.ArchEntry, error) {
	out, err := r.run(ctx, dir, "rev-list", "--first-parent", "--max-count="+strconv.Itoa(maxArchHistory), ref)
	if err != nil {
		return nil, err
	}
	var entries []org.ArchEntry
	for _, commit := range strings.Fields(out) {
		raw, err := r.output(ctx, dir, "show", commit+":"+oabranch.RepositoryPath)
		if err != nil {
			entries = append(entries, org.ArchEntry{Commit: commit})
			continue
		}
		entries = append(entries, org.ArchEntry{Commit: commit, IndexedCommit: indexedCommit([]byte(raw))})
	}
	return entries, nil
}

func indexedCommit(data []byte) string {
	doc := struct {
		Status struct {
			IndexedCommit string `json:"indexedCommit"`
		} `json:"status"`
	}{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ""
	}
	return doc.Status.IndexedCommit
}
