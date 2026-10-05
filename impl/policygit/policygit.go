package policygit

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"testing/fstest"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

const GitAttributes = "dist/** linguist-generated\nreports/** linguist-generated\nCATALOGUE.md linguist-generated\n"

func Tip(ctx context.Context, store oagit.Store, ref string) (string, error) {
	return store.Tip(ctx, ref)
}

func Read(ctx context.Context, store oagit.Store, ref string) (policy.Library, string, error) {
	commit, err := store.Tip(ctx, ref)
	if err != nil {
		return policy.Library{}, "", err
	}
	if commit == "" {
		return policy.Library{}, "", nil
	}
	files, err := store.ReadFiles(ctx, commit)
	if err != nil {
		return policy.Library{}, "", err
	}
	library, err := policyeval.LoadFS(FS(files))
	if err != nil {
		return policy.Library{}, "", err
	}
	library.Files = files
	return library, commit, nil
}

func FS(files map[string][]byte) fs.FS {
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: data}
	}
	return fsys
}

func Init(ctx context.Context, store oagit.Store, repository, ref string, manifest policy.PolicyLibrary, lib, libTest string) (string, bool, error) {
	if manifest.Repository == "" {
		manifest.Repository = repository
	}
	doc, err := yaml.Marshal(manifest)
	if err != nil {
		return "", false, err
	}
	files := map[string][]byte{
		policy.PoliciesPath:      doc,
		policy.LibPath:           []byte(lib),
		policy.LibTestPath:       []byte(libTest),
		policy.GitAttributesPath: []byte(GitAttributes),
	}
	before, err := store.Tip(ctx, ref)
	if err != nil {
		return "", false, err
	}
	message := fmt.Sprintf("policy(%s): init %s\n", repository, ref)
	commit, err := Update(ctx, store, ref, files, nil, message)
	if err != nil {
		return "", false, err
	}
	return commit, commit != before, nil
}

func Update(ctx context.Context, store oagit.Store, ref string, add map[string][]byte, remove []string, message string) (string, error) {
	parent, err := store.Tip(ctx, ref)
	if err != nil {
		return "", err
	}
	files, err := store.ReadFiles(ctx, parent)
	if err != nil {
		return "", err
	}
	for _, name := range remove {
		delete(files, name)
	}
	for name, data := range add {
		files[name] = data
	}
	blobs, err := store.Blobs(ctx, parent)
	if err != nil {
		return "", err
	}
	plan := oabranch.PlanCommit(blobs, files)
	if plan.Empty() {
		return parent, nil
	}
	return store.Commit(ctx, ref, parent, plan, message)
}

func WriteChange(ctx context.Context, store oagit.Store, ref, name string, document []byte, message string) (string, error) {
	return Update(ctx, store, ref, map[string][]byte{policy.ChangePath(name): document}, nil, message)
}

func WriteReport(ctx context.Context, store oagit.Store, ref, codeBranch, repository string, document []byte) (string, error) {
	message := fmt.Sprintf("policy(%s): report %s\n", repository, codeBranch)
	return Update(ctx, store, ref, map[string][]byte{policy.ReportPath(codeBranch): document}, nil, message)
}

func Paths(ctx context.Context, store oagit.Store, ref string) ([]string, error) {
	commit, err := store.Tip(ctx, ref)
	if err != nil || commit == "" {
		return nil, err
	}
	return store.Paths(ctx, commit)
}

func IsPolicyPath(name string) bool {
	cleaned := path.Clean(name)
	return cleaned == policy.PoliciesPath ||
		cleaned == policy.CataloguePath ||
		cleaned == policy.GitAttributesPath ||
		hasPrefix(cleaned, policy.LibPath) ||
		hasPrefix(cleaned, policy.TemplatesDir+"/") ||
		hasPrefix(cleaned, policy.ConstraintsDir+"/") ||
		hasPrefix(cleaned, policy.TestsDir+"/") ||
		hasPrefix(cleaned, policy.DistDir+"/") ||
		hasPrefix(cleaned, policy.ReportsDir+"/") ||
		hasPrefix(cleaned, policy.ChangesDir+"/")
}

func hasPrefix(name, prefix string) bool {
	return len(name) >= len(prefix) && name[:len(prefix)] == prefix
}
