package orggit

import (
	"context"
	"fmt"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
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
