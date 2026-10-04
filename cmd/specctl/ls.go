package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
)

func runLs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl ls", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "table", "table or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	records, err := session.List()
	if err != nil {
		fmt.Fprintf(stderr, "specctl ls: %v\n", err)
		return exitError
	}
	switch *output {
	case "json":
		data, err := json.MarshalIndent(records, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl ls: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(data))
	case "table":
		table := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(table, "repository\tpath\tbranch\tkcp\tready\tspecd")
		for _, record := range records {
			branch := record.Branch
			if branch == "" {
				if current, err := session.CurrentBranch(record.Repo); err == nil {
					branch = current + " (legacy record)"
				} else {
					branch = "(legacy record)"
				}
			}
			ready := "no"
			if kcpproc.Ready(instanceOf(record)) {
				ready = "yes"
			}
			specd := "stopped"
			if session.Alive(record.SpecdPid, "specd") {
				specd = fmt.Sprintf("running, pid %d", record.SpecdPid)
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", record.Repository, record.Repo, branch, record.KcpURL, ready, specd)
		}
		table.Flush()
	default:
		fmt.Fprintf(stderr, "specctl ls: unknown output %q\n", *output)
		return exitUsage
	}
	return exitOK
}
