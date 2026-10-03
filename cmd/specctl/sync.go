package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/mirror"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/specsync"
)

// runSync mirrors one repository's specs to and from `<repo>/.specs/*.yaml`, so
// a pull request carries the spec and the code together.
func runSync(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", "", "working tree of the repository to mirror")
	repository := fs.String("repository", "", "Repository name; default is the one whose resolved path is --repo")
	direction := fs.String("direction", "both", "pull, push or both")
	prefer := fs.String("prefer", "", "which side wins a conflict: kcp or git; empty refuses")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *repo == "" {
		fmt.Fprintln(stderr, "specctl sync: --repo is required")
		return exitUsage
	}
	parsedDirection, err := mirror.ParseDirection(*direction)
	if err != nil {
		fmt.Fprintf(stderr, "specctl sync: %v\n", err)
		return exitUsage
	}
	parsedPrefer, err := mirror.ParsePrefer(*prefer)
	if err != nil {
		fmt.Fprintf(stderr, "specctl sync: %v\n", err)
		return exitUsage
	}
	dir, err := filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl sync: %v\n", err)
		return exitError
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fmt.Fprintf(stderr, "specctl sync: %s is not a directory\n", dir)
		return exitError
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl sync: %v\n", err)
		return exitError
	}
	name := *repository
	if name == "" {
		name, err = repositoryForPath(ctx, client, options.namespace, dir)
		if err != nil {
			fmt.Fprintf(stderr, "specctl sync: %v\n", err)
			return exitError
		}
	}

	result, err := specsync.Run(ctx, specsync.Options{
		Cluster:    client,
		Namespace:  options.namespace,
		Repository: name,
		Dir:        dir,
		Direction:  parsedDirection,
		Prefer:     parsedPrefer,
	})
	for _, record := range result.Records {
		action := "skip"
		if record.Conflict {
			action = "conflict"
		} else if record.Pull && record.Push {
			action = "push+pull"
		} else if record.Pull {
			action = "pull"
		} else if record.Push {
			action = "push"
		}
		fmt.Fprintf(stdout, "%-12s %-10s %s\n", record.Context, action, record.Note)
	}
	if err != nil {
		fmt.Fprintf(stderr, "specctl sync: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "%s: %d contexts, %d pulled, %d pushed, %d skipped\n",
		name, result.Checked, result.Pulled, result.Pushed, result.Skipped)
	return exitOK
}

// repositoryForPath is the Repository whose resolved tree is this directory. It
// is how `specctl sync --repo <path>` finds the name the SystemContexts carry
// without asking for it twice.
func repositoryForPath(ctx context.Context, client *kcpclient.Client, namespace, dir string) (string, error) {
	listed, err := client.List(ctx, specapi.RepositoryGVR, namespace)
	if err != nil {
		return "", err
	}
	var names []string
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return "", err
		}
		repository, ok := typed.(*spec.Repository)
		if !ok {
			continue
		}
		work := repository.WorkPath()
		if work == "" {
			continue
		}
		absolute, err := filepath.Abs(work)
		if err != nil {
			continue
		}
		if absolute == dir {
			return repository.Name, nil
		}
		names = append(names, fmt.Sprintf("%s (%s)", repository.Name, absolute))
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no Repository resolves to %s; apply one first", dir)
	}
	return "", fmt.Errorf("no Repository resolves to %s; known: %s", dir, strings.Join(names, ", "))
}
