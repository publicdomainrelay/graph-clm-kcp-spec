package main

import (
	"flag"
	"fmt"
	"io"
)

var buildCommit = "unknown"

var buildDirty = ""

func runVersion(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the recorded commit as JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *asJSON {
		fmt.Fprintf(stdout, "{\"commit\":%q,\"dirty\":%t}\n", buildCommit, buildDirty != "")
		return exitOK
	}
	fmt.Fprintf(stdout, "commit %s dirty=%t\n", buildCommit, buildDirty != "")
	return exitOK
}
