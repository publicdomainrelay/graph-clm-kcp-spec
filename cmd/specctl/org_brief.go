package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/orggit"
)

// runOrgBrief prints what an agent started at an org root needs: the member
// table, where each spec lives, and the order commits and pushes go in. It is
// generated from the checkout, so it is never stale; a root CLAUDE.md can
// include its output.
func runOrgBrief(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl org brief", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "any directory of the org root")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	root, ok := openOrg(ctx, *repo, stderr, "brief")
	if !ok {
		return exitError
	}
	states, err := root.Members(ctx, orggit.MembersOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "specctl org brief: %v\n", err)
		return exitError
	}
	fmt.Fprint(stdout, orgBrief(root.Name, root.ArchBranch(ctx), root.PolicyBranch(ctx), states))
	return exitOK
}

func orgBrief(name, archBranch, policyBranch string, states []org.MemberState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Org root %s\n\n", name)
	fmt.Fprintf(&b, "This directory is a git superproject. Each submodule below is its own repository with its own\n")
	fmt.Fprintf(&b, "history, tests, specs and policies; the root records which commit of each one is current.\n\n")

	b.WriteString("## Members\n\n| path | repository | pin | checkout | spec | policy |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, state := range states {
		fmt.Fprintf(&b, "| `%s` | %s | `%s` | %s | %s | %s |\n", state.Path, state.Name, org.Short(state.CodeCommit),
			checkoutSummary(state), specSummary(state), policySummary(state))
	}

	b.WriteString("\n## Where the specs and policies are\n\n")
	fmt.Fprintf(&b, "- The root's own spec is the orphan branch `%s`, its policies `%s`. `members.yaml` on the\n", archBranch, policyBranch)
	b.WriteString("  architecture branch is a list of references; a member's spec is never copied into the root.\n")
	b.WriteString("- A member's spec is the orphan branch `open-architecture/<repository>` of that member's own repository,\n")
	b.WriteString("  its policies `open-policy/<repository>`. Read them in place, at the commit the root pins:\n")
	b.WriteString("  `specctl org outline --member <repository>`. Do not look for them in the work tree: orphan\n")
	b.WriteString("  branches are never checked out.\n")
	b.WriteString("- Cross-repository invariants are policies of the root's library and are evaluated over all members at\n")
	b.WriteString("  once (`submodules.members` in `policies.yaml`); a member's own policies are evaluated on that member.\n")

	b.WriteString("\n## Working across repositories\n\n")
	b.WriteString("1. `specctl org status` first. Fix errors (an unpublished pin) before anything else.\n")
	b.WriteString("2. Change a member inside its directory, on its own branch, and commit there with that repository's\n")
	b.WriteString("   conventions and tests. Never edit another member's files from a change meant for one member.\n")
	b.WriteString("3. Push the member first. A root commit that pins a commit no remote has is a dangling pointer.\n")
	b.WriteString("4. Record the move in the root last: `specctl org bump --change <name> <member>...`. One root commit\n")
	b.WriteString("   may bump several members; its `Member:` trailers say which commit of which repository.\n")
	b.WriteString("5. A change that touches several members is a plan: `specctl org run --plan <file> ...` commits in each\n")
	b.WriteString("   member and restores them all if one step fails.\n")
	b.WriteString("6. To see how work crossed repositories: `specctl org history [--member <repository>]` lists the root\n")
	b.WriteString("   commits that moved a pointer, the member commits each brought in and the member spec each\n")
	b.WriteString("   resolves to. A shallow clone needs `specctl org fetch` before the member branches exist.\n")

	b.WriteString("\n## Never\n\n")
	b.WriteString("- Never commit a pointer you did not mean to move: `git add -A` in the root stages every submodule that\n")
	b.WriteString("  is ahead. Use `specctl org bump <member>`.\n")
	b.WriteString("- Never force-push or rewrite a member commit the root already pins.\n")
	b.WriteString("- Never copy a member's code or spec into the root, or the root's policies into a member.\n")

	var problems []string
	for _, state := range states {
		for _, problem := range org.Problems(state) {
			if problem.Severity != org.SeverityInfo {
				problems = append(problems, fmt.Sprintf("- %s: %s (next: `%s`)", problem.Member, problem.Message, problem.Next))
			}
		}
	}
	if len(problems) > 0 {
		b.WriteString("\n## Needs attention now\n\n" + strings.Join(problems, "\n") + "\n")
	}
	return b.String()
}
