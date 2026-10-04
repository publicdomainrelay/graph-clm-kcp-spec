package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func runRetry(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl retry", flag.ContinueOnError)
	fs.SetOutput(stderr)
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: specctl retry <systemcontext>")
		return exitUsage
	}
	name := fs.Arg(0)
	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl retry: %v\n", err)
		return exitError
	}
	object, err := client.Get(ctx, specapi.SystemContextGVR, options.namespace, name)
	if err != nil {
		fmt.Fprintf(stderr, "specctl retry: %v\n", err)
		return exitError
	}
	changes, err := client.List(ctx, specapi.SpecChangeGVR, options.namespace)
	if err != nil {
		fmt.Fprintf(stderr, "specctl retry: %v\n", err)
		return exitError
	}
	cleared := 0
	for index := range changes.Items {
		typed, err := kcpclient.Typed(&changes.Items[index])
		if err != nil {
			continue
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok || change.Spec.SystemContext != name || change.Status.Phase != specapi.PhaseFailed {
			continue
		}
		if err := client.Delete(ctx, specapi.SpecChangeGVR, options.namespace, change.Name); err != nil {
			fmt.Fprintf(stderr, "specctl retry: %v\n", err)
			return exitError
		}
		cleared++
	}
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[specapi.RetryRequestAnnotation] = strconv.FormatInt(time.Now().UnixNano(), 10)
	object.SetAnnotations(annotations)
	if _, err := client.Update(ctx, object); err != nil {
		fmt.Fprintf(stderr, "specctl retry: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "%s: cleared %d failed change(s); specd raises a fresh attempt for what is still unrealized\n", name, cleared)
	return exitOK
}
