package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/orggit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/orgrealize"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

const orgUsage = `specctl org: work across the repositories of an org root (a git superproject).

usage:
  specctl org ls [--repo .] [-o table|json]
      the submodules: pin, checkout state, where each one's spec is
  specctl org status [--repo .] [-o text|json]
      what to fix before the root can be trusted or pushed; exits 1 on an error
  specctl org history [--repo .] [--member <name>] [-n 20] [--ref HEAD] [-o text|json]
      the root commits that moved a pointer, the member commits each brought in
  specctl org manifest [--repo .] [--write] [-o yaml|json]
      members.yaml of the root's architecture branch; --write commits it there
  specctl org outline [--repo .] [--member <name>] [-o text|json]
      the root's architecture, or one member's read at the recorded commit
  specctl org fetch [--repo .]
      fetch every member's architecture and policy branches and missing pins
  specctl org clone <url> [<dir>] [--depth <n>]
      recursive clone, then fetch the members' architecture and policy branches
  specctl org bump [--repo .] [--change <name>] [--message <text>] [--allow-unpublished] [<member>...]
      commit the new pins of members whose checkout moved
  specctl org run --plan <file> --agent scripted:<file> [--repo .] [--push] [--root-branch <name>]
      a change across repositories: each step in its member, then one root commit
  specctl org brief [--repo .]
      what an agent started at the org root needs to know
`

func runOrg(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stderr, orgUsage)
		return exitUsage
	}
	command, rest := args[0], args[1:]
	switch command {
	case "ls":
		return runOrgLs(rest, stdout, stderr)
	case "status":
		return runOrgStatus(rest, stdout, stderr)
	case "history":
		return runOrgHistory(rest, stdout, stderr)
	case "manifest":
		return runOrgManifest(rest, stdout, stderr)
	case "outline":
		return runOrgOutline(rest, stdout, stderr)
	case "fetch":
		return runOrgFetch(rest, stdout, stderr)
	case "clone":
		return runOrgClone(rest, stdout, stderr)
	case "bump":
		return runOrgBump(rest, stdout, stderr)
	case "run":
		return runOrgRun(rest, stdout, stderr)
	case "brief":
		return runOrgBrief(rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "specctl org: unknown command %q\n%s", command, orgUsage)
	return exitUsage
}

func openOrg(ctx context.Context, repo string, stderr io.Writer, command string) (*orggit.Root, bool) {
	root, err := orggit.Open(ctx, repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl org %s: %v\n", command, err)
		return nil, false
	}
	if !root.HasSubmodules(ctx) {
		fmt.Fprintf(stderr, "specctl org %s: %s has no submodules; it is not an org root\n", command, root.Dir)
		return nil, false
	}
	return root, true
}

func writeJSON(stdout, stderr io.Writer, command string, value any) int {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "specctl org %s: %v\n", command, err)
		return exitError
	}
	fmt.Fprintln(stdout, string(data))
	return exitOK
}

func runOrgLs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org ls", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	output := fs.String("o", "table", "table or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "ls")
	if !ok {
		return exitError
	}
	states, err := root.Members(ctx, orggit.MembersOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "specctl org ls: %v\n", err)
		return exitError
	}
	if *output == "json" {
		return writeJSON(stdout, stderr, "ls", states)
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "member\tpath\tpin\tcheckout\tspec\tpolicy")
	for _, state := range states {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", state.Name, state.Path, org.Short(state.CodeCommit),
			checkoutSummary(state), specSummary(state), policySummary(state))
	}
	return exitOKFlush(table)
}

func exitOKFlush(table *tabwriter.Writer) int {
	table.Flush()
	return exitOK
}

func checkoutSummary(state org.MemberState) string {
	if !state.Initialized {
		return "not checked out"
	}
	parts := []string{}
	switch {
	case state.Head == state.CodeCommit:
		parts = append(parts, "at pin")
	case state.Ahead > 0 || state.Behind > 0:
		parts = append(parts, fmt.Sprintf("%d ahead, %d behind", state.Ahead, state.Behind))
	default:
		parts = append(parts, "off pin")
	}
	if state.Dirty {
		parts = append(parts, "dirty")
	}
	if state.PinPresent && !state.Published {
		parts = append(parts, "unpublished")
	}
	return strings.Join(parts, ", ")
}

func specSummary(state org.MemberState) string {
	if state.Arch == nil {
		return string(state.State)
	}
	return fmt.Sprintf("%s @%s", state.State, org.Short(state.Arch.Commit))
}

func policySummary(state org.MemberState) string {
	if state.Policy == nil {
		return "-"
	}
	return "@" + org.Short(state.Policy.Commit)
}

type orgStatusReport struct {
	Root     string         `json:"root"`
	Branch   string         `json:"branch,omitempty"`
	Members  int            `json:"members"`
	Problems []org.Problem  `json:"problems"`
	Counts   map[string]int `json:"counts"`
}

func runOrgStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	output := fs.String("o", "text", "text or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "status")
	if !ok {
		return exitError
	}
	states, err := root.Members(ctx, orggit.MembersOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "specctl org status: %v\n", err)
		return exitError
	}
	report := orgStatusReport{Root: root.Name, Branch: root.CurrentBranch(ctx), Members: len(states), Problems: []org.Problem{}, Counts: map[string]int{}}
	failed := false
	for _, state := range states {
		for _, problem := range org.Problems(state) {
			report.Problems = append(report.Problems, problem)
			report.Counts[string(problem.Severity)]++
			failed = failed || problem.Severity == org.SeverityError
		}
	}
	if *output == "json" {
		if code := writeJSON(stdout, stderr, "status", report); code != exitOK {
			return code
		}
	} else {
		fmt.Fprintf(stdout, "org root %s on %s: %d member(s)\n", report.Root, valueOr(report.Branch, "(detached)"), report.Members)
		if len(report.Problems) == 0 {
			fmt.Fprintln(stdout, "clean: every pin is published, every checkout is at its pin, every member has a spec")
		}
		for _, problem := range report.Problems {
			fmt.Fprintf(stdout, "%-5s %-24s %s\n", problem.Severity, problem.Member, problem.Message)
			if problem.Next != "" {
				fmt.Fprintf(stdout, "      next: %s\n", problem.Next)
			}
		}
	}
	if failed {
		return exitError
	}
	return exitOK
}

func runOrgHistory(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org history", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	member := fs.String("member", "", "only the moves of this member")
	limit := fs.Int("n", 20, "root commits to show")
	ref := fs.String("ref", "HEAD", "root ref to walk from")
	output := fs.String("o", "text", "text or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "history")
	if !ok {
		return exitError
	}
	entries, err := root.History(ctx, orggit.HistoryOptions{Ref: *ref, Member: *member, Limit: *limit})
	if err != nil {
		fmt.Fprintf(stderr, "specctl org history: %v\n", err)
		return exitError
	}
	if *output == "json" {
		return writeJSON(stdout, stderr, "history", entries)
	}
	for _, entry := range entries {
		fmt.Fprintf(stdout, "%s  %s  %s\n", org.Short(entry.Commit), entry.Date, entry.Subject)
		for _, move := range entry.Details {
			switch {
			case move.Added():
				fmt.Fprintf(stdout, "  + %s at %s\n", move.Member, org.Short(move.To))
			case move.Removed():
				fmt.Fprintf(stdout, "  - %s (was %s)\n", move.Member, org.Short(move.From))
			default:
				fmt.Fprintf(stdout, "  ~ %s %s..%s", move.Member, org.Short(move.From), org.Short(move.To))
				if move.Total > 0 {
					fmt.Fprintf(stdout, "  %d commit(s)", move.Total)
				}
				if move.Arch != "" {
					fmt.Fprintf(stdout, "  spec @%s", org.Short(move.Arch))
				}
				if move.Message != "" {
					fmt.Fprintf(stdout, "  (%s)", move.Message)
				}
				fmt.Fprintln(stdout)
				for _, line := range move.Commits {
					fmt.Fprintf(stdout, "      %s %s\n", org.Short(line.Commit), line.Subject)
				}
				if move.Total > len(move.Commits) {
					fmt.Fprintf(stdout, "      ... %d more\n", move.Total-len(move.Commits))
				}
			}
		}
		for _, bump := range entry.Bumps {
			if bump.Spec != "" {
				fmt.Fprintf(stdout, "  trailer: %s spec %s\n", bump.Member, org.Short(bump.Spec))
			}
		}
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "no commit of this history moved a submodule pointer")
	}
	return exitOK
}

func runOrgManifest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org manifest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	write := fs.Bool("write", false, "commit members.yaml to the root's architecture branch")
	output := fs.String("o", "yaml", "yaml or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "manifest")
	if !ok {
		return exitError
	}
	if *write {
		commit, changed, err := root.WriteManifest(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "specctl org manifest: %v\n", err)
			return exitError
		}
		state := "unchanged"
		if changed {
			state = "committed"
		}
		fmt.Fprintf(stdout, "%s %s on %s at %s\n", org.MembersPath, state, root.ArchBranch(ctx), org.Short(commit))
		return exitOK
	}
	manifest, _, err := root.Manifest(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl org manifest: %v\n", err)
		return exitError
	}
	if *output == "json" {
		return writeJSON(stdout, stderr, "manifest", manifest)
	}
	data, err := manifest.Render()
	if err != nil {
		fmt.Fprintf(stderr, "specctl org manifest: %v\n", err)
		return exitError
	}
	fmt.Fprint(stdout, string(data))
	return exitOK
}

type orgOutline struct {
	Repository string         `json:"repository"`
	Source     string         `json:"source"`
	Contexts   []outlineEntry `json:"contexts"`
	Members    []org.Member   `json:"members,omitempty"`
}

func runOrgOutline(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org outline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	member := fs.String("member", "", "outline this member's architecture instead of the root's")
	output := fs.String("o", "text", "text or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "outline")
	if !ok {
		return exitError
	}
	states, err := root.Members(ctx, orggit.MembersOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "specctl org outline: %v\n", err)
		return exitError
	}
	var outline orgOutline
	if *member != "" {
		var target *org.MemberState
		for index := range states {
			if states[index].Name == *member || states[index].Path == *member {
				target = &states[index]
			}
		}
		if target == nil {
			fmt.Fprintf(stderr, "specctl org outline: %q is not a member\n", *member)
			return exitError
		}
		files, err := root.ReadArch(ctx, target.Member, nil)
		if err != nil {
			fmt.Fprintf(stderr, "specctl org outline: %v\n", err)
			return exitError
		}
		contexts, err := outlineFromFiles(files)
		if err != nil {
			fmt.Fprintf(stderr, "specctl org outline: %v\n", err)
			return exitError
		}
		outline = orgOutline{Repository: target.Name, Source: fmt.Sprintf("%s@%s", target.Arch.Branch, org.Short(target.Arch.Commit)), Contexts: contexts}
	} else {
		outline = orgOutline{Repository: root.Name}
		for _, state := range states {
			outline.Members = append(outline.Members, state.Member)
		}
		tip, err := root.Store().Tip(ctx, "refs/heads/"+root.ArchBranch(ctx))
		if err == nil && tip != "" {
			files, err := root.Store().ReadFiles(ctx, tip)
			if err != nil {
				fmt.Fprintf(stderr, "specctl org outline: %v\n", err)
				return exitError
			}
			if outline.Contexts, err = outlineFromFiles(files); err != nil {
				fmt.Fprintf(stderr, "specctl org outline: %v\n", err)
				return exitError
			}
			outline.Source = fmt.Sprintf("%s@%s", root.ArchBranch(ctx), org.Short(tip))
		} else {
			outline.Source = "no " + root.ArchBranch(ctx) + " branch yet"
		}
	}
	if *output == "json" {
		return writeJSON(stdout, stderr, "outline", outline)
	}
	fmt.Fprintf(stdout, "%s  (%s)\n", outline.Repository, outline.Source)
	for _, entry := range outline.Contexts {
		fmt.Fprintf(stdout, "  %s  requirements=%d\n", entry.Name, entry.Requirements)
		if entry.Intent != "" {
			fmt.Fprintf(stdout, "    intent: %s\n", firstSentence(entry.Intent, 160))
		}
	}
	for _, m := range outline.Members {
		fmt.Fprintf(stdout, "  member %s  %s  pin %s  spec: %s", m.Name, m.Path, org.Short(m.CodeCommit), m.State)
		if m.Arch != nil {
			fmt.Fprintf(stdout, " @%s  (specctl org outline --member %s)", org.Short(m.Arch.Commit), m.Name)
		}
		fmt.Fprintln(stdout)
	}
	return exitOK
}

func outlineFromFiles(files map[string][]byte) ([]outlineEntry, error) {
	specs, err := oabranch.SpecFiles(files)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]outlineEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, outlineOf(specs[name]))
	}
	return entries, nil
}

func runOrgFetch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org fetch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "fetch")
	if !ok {
		return exitError
	}
	results, err := root.Fetch(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl org fetch: %v\n", err)
		return exitError
	}
	printFetch(stdout, results)
	return exitOK
}

func printFetch(stdout io.Writer, results []orggit.FetchResult) {
	for _, result := range results {
		line := fmt.Sprintf("%s: %d branch(es)", result.Member, len(result.Branches))
		if result.PinFetched {
			line += ", pin fetched"
		}
		if result.Message != "" {
			line += "  [" + result.Message + "]"
		}
		fmt.Fprintln(stdout, line)
	}
}

func runOrgClone(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org clone", flag.ContinueOnError)
	fs.SetOutput(stderr)
	depth := fs.Int("depth", 0, "shallow clone depth for the root and every member; 0 is a full clone")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) == 0 || len(positional) > 2 {
		fmt.Fprintln(stderr, "specctl org clone: <url> [<dir>]")
		return exitUsage
	}
	dir := ""
	if len(positional) == 2 {
		dir = positional[1]
	} else {
		dir = strings.TrimSuffix(filepath.Base(strings.TrimRight(positional[0], "/")), ".git")
	}
	ctx := context.Background()
	root, err := orggit.Clone(ctx, positional[0], dir, orggit.CloneOptions{Depth: *depth})
	if err != nil {
		fmt.Fprintf(stderr, "specctl org clone: %v\n", err)
		return exitError
	}
	states, err := root.Members(ctx, orggit.MembersOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "specctl org clone: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "cloned %s: %d member(s)\n", root.Dir, len(states))
	for _, state := range states {
		fmt.Fprintf(stdout, "  %-32s %s  spec: %s\n", state.Path, org.Short(state.CodeCommit), specSummary(state))
	}
	return exitOK
}

func runOrgBump(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org bump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	change := fs.String("change", "", "the Spec-Change trailer")
	message := fs.String("message", "", "body of the root commit")
	allow := fs.Bool("allow-unpublished", false, "record a pin no remote has yet")
	names, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "bump")
	if !ok {
		return exitError
	}
	commit, bumps, err := root.Bump(ctx, names, orggit.BumpOptions{Change: *change, Body: *message, AllowUnpublished: *allow})
	if err != nil {
		fmt.Fprintf(stderr, "specctl org bump: %v\n", err)
		return exitError
	}
	if len(bumps) == 0 {
		fmt.Fprintln(stdout, "nothing to bump: every checkout is at its pin")
		return exitOK
	}
	fmt.Fprintf(stdout, "root commit %s\n", org.Short(commit))
	for _, bump := range bumps {
		fmt.Fprintf(stdout, "  %s %s -> %s\n", bump.Member, org.Short(bump.From), org.Short(bump.To))
	}
	return exitOK
}

func runOrgRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	planFile := fs.String("plan", "", "the change plan (yaml or json)")
	agentSpec := fs.String("agent", "", "scripted:<scenario file>")
	push := fs.Bool("push", false, "push each member's change branch before the root records its pin")
	rootBranch := fs.String("root-branch", "", "commit the pointers on this new root branch")
	output := fs.String("o", "text", "text or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	scenarioPath, scripted := strings.CutPrefix(*agentSpec, "scripted:")
	if *planFile == "" || !scripted || scenarioPath == "" {
		fmt.Fprintln(stderr, "specctl org run: --plan and --agent scripted:<file> are required")
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "run")
	if !ok {
		return exitError
	}
	data, err := os.ReadFile(*planFile)
	if err != nil {
		fmt.Fprintf(stderr, "specctl org run: %v\n", err)
		return exitError
	}
	plan := orgrealize.Plan{}
	if err := yaml.UnmarshalStrict(data, &plan); err != nil {
		fmt.Fprintf(stderr, "specctl org run: parse %s: %v\n", *planFile, err)
		return exitError
	}
	scenario, err := scriptedagent.Load(scenarioPath)
	if err != nil {
		fmt.Fprintf(stderr, "specctl org run: %v\n", err)
		return exitError
	}
	result, err := orgrealize.Run(ctx, orgrealize.Options{
		Root: root, Agent: scriptedagent.New(scenario), Push: *push, RootBranch: *rootBranch,
	}, plan)
	if err != nil {
		fmt.Fprintf(stderr, "specctl org run: %v\n", err)
		return exitError
	}
	if *output == "json" {
		return writeJSON(stdout, stderr, "run", result)
	}
	for _, step := range result.Steps {
		fmt.Fprintf(stdout, "%s: %s -> %s on %s (%d file(s))\n", step.Member, org.Short(step.From), org.Short(step.To), step.Branch, len(step.Files))
	}
	fmt.Fprintf(stdout, "root commit %s\n", org.Short(result.RootCommit))
	if len(result.Unpublished) > 0 {
		fmt.Fprintf(stdout, "not pushed: %s; push the members before the root\n", strings.Join(result.Unpublished, ", "))
	}
	return exitOK
}
