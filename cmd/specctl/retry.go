package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func runRetry(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl retry", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "manual retry", "why the change is retried; recorded on the new attempt")
	by := fs.String("by", "", "who asked for the retry; defaults to the environment's user")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: specctl retry [--reason <text>] [--by <user>] <systemcontext>")
		return exitUsage
	}
	name := fs.Arg(0)
	user := *by
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		user = "specctl"
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl retry: %v\n", err)
		return exitError
	}
	if _, err := client.Get(ctx, specapi.SystemContextGVR, options.namespace, name); err != nil {
		fmt.Fprintf(stderr, "specctl retry: %v\n", err)
		return exitError
	}
	changes, err := changesOf(ctx, client, options.namespace)
	if err != nil {
		fmt.Fprintf(stderr, "specctl retry: %v\n", err)
		return exitError
	}

	attempts := spec.RetryAttempts(changes, name)
	if len(attempts) == 0 {
		fmt.Fprintf(stdout, "%s: no failed change to retry\n", name)
		return exitOK
	}
	for _, attempt := range attempts {
		change := &spec.SpecChange{
			TypeMeta: metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.SpecChangeKind},
			ObjectMeta: metav1.ObjectMeta{
				Name:      attempt.Name,
				Namespace: options.namespace,
			},
			Spec: attempt.Source.Spec,
		}
		object, err := kcpclient.Unstructured(change)
		if err != nil {
			fmt.Fprintf(stderr, "specctl retry: %v\n", err)
			return exitError
		}
		if _, err := client.Apply(ctx, object); err != nil {
			fmt.Fprintf(stderr, "specctl retry: %v\n", err)
			return exitError
		}
		status := map[string]any{
			"phase":       specapi.PhasePending,
			"attempt":     attempt.Attempt,
			"retryReason": *reason,
			"retryBy":     user,
			"message":     fmt.Sprintf("attempt %d retried by %s (%s)", attempt.Attempt, user, *reason),
		}
		if _, err := client.PatchStatus(ctx, specapi.SpecChangeGVR, options.namespace, attempt.Name, status); err != nil {
			fmt.Fprintf(stderr, "specctl retry: %v\n", err)
			return exitError
		}
		fmt.Fprintf(stdout, "%s: attempt %d of %s: %s\n", name, attempt.Attempt, attempt.Base, attempt.Name)
	}
	fmt.Fprintf(stdout, "%s: retried %d change(s) by %s (%s); specd raises nothing else for what is unrealized\n",
		name, len(attempts), user, *reason)
	return exitOK
}

func changesOf(ctx context.Context, client *kcpclient.Client, namespace string) ([]spec.SpecChange, error) {
	listed, err := client.List(ctx, specapi.SpecChangeGVR, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]spec.SpecChange, 0, len(listed.Items))
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			continue
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok {
			continue
		}
		out = append(out, *change)
	}
	return out, nil
}
