package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
)

// observedContext is one context as path resolution sees it: the name kcp gives
// it and the files it observes.
type observedContext struct {
	name  string
	files []string
}

// ownerOfPath names the context that owns a path: the deepest match wins, and
// the smallest of the equally deep ones, because the smallest is the context
// the path is most specific to. The path is read as the checkout holds it. A
// path no observed file matches is owned by no context.
func ownerOfPath(path string, contexts []observedContext) string {
	wanted := checkoutPath(path)
	if wanted == "" {
		return ""
	}
	owner, depth, size := "", 0, 0
	for _, candidate := range contexts {
		matched := matchDepth(wanted, candidate.files)
		if matched == 0 || matched < depth {
			continue
		}
		if matched == depth && owner != "" && (len(candidate.files) > size ||
			(len(candidate.files) == size && candidate.name >= owner)) {
			continue
		}
		owner, depth, size = candidate.name, matched, len(candidate.files)
	}
	return owner
}

// matchDepth is how deep a path matches the files one context observes, in path
// segments, and 0 when it matches none of them. An observed file that is the
// path itself or a directory the path is under is the deepest kind of match; an
// observed file under the path is a match of the path's own depth, so a
// directory resolves to the context that holds it.
func matchDepth(path string, files []string) int {
	depth := 0
	for _, file := range files {
		entry := checkoutPath(file)
		if entry == "" {
			continue
		}
		switch {
		case entry == path, strings.HasPrefix(path, entry+"/"):
			depth = max(depth, segments(entry))
		case strings.HasPrefix(entry, path+"/"):
			depth = max(depth, segments(path))
		}
	}
	return depth
}

func segments(path string) int {
	count := 0
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			count++
		}
	}
	return count
}

// checkoutPath reads a path the way the observed files are held: forward
// slashes, no leading ./, no trailing slash. An empty path names no file.
func checkoutPath(path string) string {
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

// relativeToCheckout reads the path the contexts observe: observed files are
// recorded relative to the checkout root, so an absolute path inside the
// checkout is made relative to it. A path outside the checkout names no
// observed file.
func relativeToCheckout(repository *spec.Repository, path string) string {
	cleaned := checkoutPath(path)
	if cleaned == "" || !filepath.IsAbs(filepath.FromSlash(cleaned)) {
		return cleaned
	}
	if repository != nil {
		if root := repository.WorkPath(); root != "" {
			relative, err := filepath.Rel(root, filepath.FromSlash(cleaned))
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return ""
			}
			return checkoutPath(relative)
		}
	}
	return ""
}

func runArchOwner(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl arch owner", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repository", "", "Repository whose contexts to resolve against; default is this repository's session, else every one")
	output := fs.String("o", "text", "text or json")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "specctl arch owner: one path is required")
		return exitUsage
	}
	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl arch owner: %v\n", err)
		return exitError
	}
	name := *repository
	if name == "" {
		if record, ok := session.ForDir("."); ok {
			name = record.Repository
		}
	}
	holder, err := readRepositoryNamed(ctx, client, options.namespace, name)
	if err != nil {
		fmt.Fprintf(stderr, "specctl arch owner: %v\n", err)
		return exitError
	}
	listed, err := client.List(ctx, specapi.SystemContextGVR, options.namespace)
	if err != nil {
		fmt.Fprintf(stderr, "specctl arch owner: %v\n", err)
		return exitError
	}
	contexts := []observedContext{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			fmt.Fprintf(stderr, "specctl arch owner: %v\n", err)
			return exitError
		}
		systemContext, ok := typed.(*spec.SystemContext)
		if !ok || systemContext.Spec.Repository != holder.Name {
			continue
		}
		contexts = append(contexts, observedContext{
			name:  systemContext.Name,
			files: systemContext.Status.Observed.Files,
		})
	}
	path := relativeToCheckout(holder, positional[0])
	owner := ownerOfPath(path, contexts)
	if *output == "json" {
		answer := map[string]string{"path": path}
		if owner != "" {
			answer["context"] = owner
		}
		data, err := json.MarshalIndent(answer, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl arch owner: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(data))
		return exitOK
	}
	if owner == "" {
		fmt.Fprintf(stdout, "no context owns %s\n", path)
		return exitOK
	}
	fmt.Fprintln(stdout, owner)
	return exitOK
}
