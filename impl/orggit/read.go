package orggit

import (
	"context"
	"fmt"
	"sort"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
)

// MemberStore is the plumbing view of a member's own repository.
func (r *Root) MemberStore(member org.Member) oagit.Store {
	return oagit.Store{Repo: r.MemberDir(member)}
}

// ReadArch reads files of a member's architecture branch at the commit the
// root recorded, without checking anything out. Nothing is copied into the
// root. An empty paths list reads every file.
func (r *Root) ReadArch(ctx context.Context, member org.Member, paths []string) (map[string][]byte, error) {
	if member.Arch == nil || member.Arch.Commit == "" {
		return nil, fmt.Errorf("orggit: %s has no architecture commit (state %s)", member.Name, member.State)
	}
	store := r.MemberStore(member)
	if len(paths) == 0 {
		return store.ReadFiles(ctx, member.Arch.Commit)
	}
	return store.ReadFilesAt(ctx, member.Arch.Commit, paths)
}

// ReadPolicy reads files of a member's policy branch at its tip.
func (r *Root) ReadPolicy(ctx context.Context, member org.Member, paths []string) (map[string][]byte, error) {
	if member.Policy == nil || member.Policy.Commit == "" {
		return nil, fmt.Errorf("orggit: %s has no policy branch", member.Name)
	}
	store := r.MemberStore(member)
	if len(paths) == 0 {
		return store.ReadFiles(ctx, member.Policy.Commit)
	}
	return store.ReadFilesAt(ctx, member.Policy.Commit, paths)
}

// PolicyRef is the ref of a member's policy branch that resolves in its
// checkout: the local branch, else the remote-tracking one a clone leaves.
func (r *Root) PolicyRef(ctx context.Context, member org.Member) (string, bool) {
	if member.Policy == nil {
		return "", false
	}
	ref, _ := r.branchTip(ctx, r.MemberDir(member), member.Policy.Branch)
	return ref, ref != ""
}

// MemberContextLabel marks a member's context inside the root's declared model.
const MemberContextLabel = "specs.publicdomainrelay.dev/member"

// MemberContexts reads a member's declared contexts from its architecture
// branch, at the commit the root recorded, and names each `<member>/<context>`
// so one model can hold every member's contexts and the root's own. Nothing is
// copied into the root: the contexts exist only for the evaluation.
func (r *Root) MemberContexts(ctx context.Context, member org.Member) ([]spec.SystemContext, error) {
	files, err := r.ReadArch(ctx, member, nil)
	if err != nil {
		return nil, err
	}
	specs, err := oabranch.SpecFiles(files)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]spec.SystemContext, 0, len(names))
	for _, name := range names {
		context := specs[name]
		context.Name = member.Name + "/" + name
		labels := map[string]string{}
		for key, value := range context.Labels {
			labels[key] = value
		}
		labels[MemberContextLabel] = member.Name
		context.Labels = labels
		out = append(out, context)
	}
	return out, nil
}

// AllMemberContexts is MemberContexts of every member that has an architecture
// commit; a member without one contributes nothing.
func (r *Root) AllMemberContexts(ctx context.Context) ([]spec.SystemContext, error) {
	states, err := r.Members(ctx, MembersOptions{})
	if err != nil {
		return nil, err
	}
	var out []spec.SystemContext
	for _, state := range states {
		if !state.Initialized || state.Arch == nil || state.Arch.Commit == "" {
			continue
		}
		contexts, err := r.MemberContexts(ctx, state.Member)
		if err != nil {
			return nil, err
		}
		out = append(out, contexts...)
	}
	return out, nil
}
