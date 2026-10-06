package orggit

import (
	"context"
	"fmt"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
)

// Manifest builds members.yaml for the root as it is checked out.
func (r *Root) Manifest(ctx context.Context) (org.Manifest, []org.MemberState, error) {
	states, err := r.Members(ctx, MembersOptions{})
	if err != nil {
		return org.Manifest{}, nil, err
	}
	members := make([]org.Member, 0, len(states))
	for _, state := range states {
		members = append(members, state.Member)
	}
	return org.NewManifest(r.Name, r.ArchBranch(ctx), members), states, nil
}

// ManifestFiles is members.yaml as the file a persist adds to the root's
// architecture branch, so the branch's own writer never deletes it.
func (r *Root) ManifestFiles(ctx context.Context) (map[string][]byte, error) {
	if !r.HasSubmodules(ctx) {
		return nil, nil
	}
	manifest, _, err := r.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	data, err := manifest.Render()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{org.MembersPath: data}, nil
}

// WriteManifest commits members.yaml to the root's architecture branch with
// plumbing: the work tree, the index and HEAD of the root are not touched, and
// every other file of the branch is kept. It returns the commit and whether
// anything changed.
func (r *Root) WriteManifest(ctx context.Context) (string, bool, error) {
	manifest, _, err := r.Manifest(ctx)
	if err != nil {
		return "", false, err
	}
	data, err := manifest.Render()
	if err != nil {
		return "", false, err
	}
	store := r.Store()
	ref := "refs/heads/" + manifest.Metadata.Branch
	tip, err := store.Tip(ctx, ref)
	if err != nil {
		return "", false, err
	}
	files := map[string][]byte{}
	if tip != "" {
		if files, err = store.ReadFiles(ctx, tip); err != nil {
			return "", false, err
		}
	}
	files[org.MembersPath] = data
	if _, ok := files[oabranch.GitAttributesPath]; !ok {
		files[oabranch.GitAttributesPath] = []byte(org.MembersPath + " linguist-generated=true\n")
	}
	blobs, err := store.Blobs(ctx, tip)
	if err != nil {
		return "", false, err
	}
	plan := oabranch.PlanCommit(blobs, files)
	if plan.Empty() {
		return tip, false, nil
	}
	message := fmt.Sprintf("members: %d submodule reference(s)\n", len(manifest.Members))
	commit, err := store.Commit(ctx, ref, tip, plan, message)
	if err != nil {
		return "", false, err
	}
	return commit, true, nil
}

// ReadManifest reads members.yaml from the tip of the root's architecture
// branch, or reports that there is none.
func (r *Root) ReadManifest(ctx context.Context) (org.Manifest, bool, error) {
	store := r.Store()
	tip, err := store.Tip(ctx, "refs/heads/"+r.ArchBranch(ctx))
	if err != nil || tip == "" {
		return org.Manifest{}, false, err
	}
	files, err := store.ReadFilesAt(ctx, tip, []string{org.MembersPath})
	if err != nil {
		return org.Manifest{}, false, err
	}
	data, ok := files[org.MembersPath]
	if !ok {
		return org.Manifest{}, false, nil
	}
	manifest, err := org.ParseManifest(data)
	return manifest, err == nil, err
}
