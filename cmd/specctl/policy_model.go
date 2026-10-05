package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

func runPolicyModel(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy model", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repo", "", "repository name")
	worktree := fs.String("worktree", "", "code checkout to index and model")
	commit := fs.String("commit", "", "commit of --path to export and model")
	path := fs.String("path", ".", "git repository holding the policy branch")
	branch := fs.String("branch", "", "code branch")
	defaultBranch := fs.String("default-branch", "main", "default code branch")
	libraryDir := fs.String("library", "", "read the policy library from this directory instead of the branch")
	output := fs.String("o", "text", "text or json")
	propose := fs.Bool("propose-interactions", false, "print a YAML interactions block per context, proposed from the observed flows")
	cacheDir := fs.String("cache-dir", defaultCacheDir(), "where a member repository is cloned")
	memberPathFlags := memberPaths{}
	fs.Var(memberPathFlags, "member", "clone the named member from a local path instead of its url (name=path); repeatable")
	classifiers := stringsFlag{}
	fs.Var(&classifiers, "classifiers", "directory of extra classifier packs; repeatable")
	testGlobs := stringsFlag{}
	fs.Var(&testGlobs, "test-glob", "test file glob; repeatable")
	indexInPlace := fs.Bool("index-in-place", false, "write the codegraph index into the checkout instead of a copy")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *worktree != "" && *commit != "" {
		fmt.Fprintln(stderr, "specctl policy model: --worktree and --commit are exclusive")
		return exitUsage
	}
	if *repository == "" && *worktree != "" {
		*repository = repositoryForWorktree(*worktree)
	}
	if *repository == "" {
		*repository = filepath.Base(*path)
	}

	ctx := context.Background()
	library, err := loadLibrary(ctx, *repository, *libraryDir, *path, *branch, *defaultBranch)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}

	codeDir := *worktree
	cleanup := func() {}
	if *commit != "" {
		codeDir, cleanup, err = exportCommit(ctx, *path, *commit)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
			return exitError
		}
	} else if codeDir == "" {
		codeDir = *path
	}
	defer cleanup()
	if absolute, absErr := filepath.Abs(codeDir); absErr == nil {
		codeDir = absolute
	}
	resolved := *commit
	if resolved == "" {
		resolved = codegraphfacts.GitCommit(ctx, codeDir)
	}
	if len(testGlobs) == 0 {
		testGlobs = library.Manifest.TestGlobs
	}

	contexts := loadContexts(ctx, *path, *repository, *branch, *defaultBranch)
	graph, err := codegraphfacts.Build(ctx, codeDir, codegraphfacts.Options{
		Repository:   *repository,
		Branch:       *branch,
		Commit:       resolved,
		TestGlobs:    testGlobs,
		Contexts:     codegraphfacts.ContextsByFile(contexts),
		IndexInPlace: *indexInPlace,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}
	libraryClassifiers, classifierCleanup, err := policyeval.LibraryClassifierDirs(library, codeDir, "")
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}
	defer classifierCleanup()
	explicit := classifiers
	if len(explicit) == 0 {
		explicit = libraryClassifiers
	}
	computed, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: classifierDirs(codeDir, explicit),
		IncludeExtras:   true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}

	members, err := policyeval.ResolveMembers(ctx, library.Manifest.Members, library, memberOptions(*cacheDir, memberPathFlags))
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}
	defer cleanupMembers(members)
	model, _, err := policyeval.BuildEvaluationModel(ctx, policyeval.ModelRequest{
		Repository: *repository,
		Graph:      graph,
		Effects:    computed,
		Contexts:   modelContexts(contexts),
		Library:    library,
		Members:    members,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}

	if *propose {
		fmt.Fprint(stdout, proposeInteractions(model))
		return exitOK
	}
	if *output == "json" {
		encoded, err := json.MarshalIndent(model, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
		return exitOK
	}
	printModel(stdout, resolved, model, members)
	return exitOK
}

func modelContexts(contexts []spec.SystemContext) []policy.ModelContext {
	out := make([]policy.ModelContext, 0, len(contexts))
	for _, context := range contexts {
		out = append(out, policy.ModelContext{
			Name:         context.Name,
			Labels:       context.Labels,
			Interactions: declaredInteractionsOf(context.Spec.Interactions),
		})
	}
	return out
}

func declaredInteractionsOf(interactions []spec.Interaction) []policy.DeclaredInteraction {
	out := make([]policy.DeclaredInteraction, 0, len(interactions))
	for _, interaction := range interactions {
		out = append(out, policy.DeclaredInteraction{
			Peer:      interaction.Peer,
			Initiator: interaction.Initiator,
			Channel:   interaction.Channel,
			Carries:   interaction.Carries,
			Purpose:   interaction.Purpose,
			Level:     string(interaction.Level),
			Forbidden: interaction.Forbidden,
		})
	}
	return out
}

// proposeInteractions writes a pasteable interactions block per declared
// context, derived from the observed flows whose source side is one of the
// context's roles. It is a draft: the level is proposed as SHOULD and nothing
// is applied.
func proposeInteractions(model policy.ArchitectureModel) string {
	builder := &strings.Builder{}
	builder.WriteString("# Proposed interactions, derived from observed flows.\n")
	builder.WriteString("# Paste a block into the spec block of its context, set the level, then apply.\n")
	proposed := 0
	for _, component := range model.Spec.Components {
		label := component.Context
		if label == "" {
			// A role-owned file group has no context yet; propose under the
			// component's own name so the block still says where it came from.
			label = component.Name
		}
		sources := map[string]bool{component.Context: true, component.Name: true}
		for _, role := range component.Roles {
			sources[role] = true
		}
		lines := []string{}
		seen := map[string]bool{}
		for _, flow := range model.Spec.Flows {
			if !policy.Observed(flow) || !sources[flow.From] {
				continue
			}
			if unknownTarget(flow.To) {
				continue
			}
			initiator := "peer"
			if sources[flow.Initiator] {
				initiator = "self"
			}
			proposal := policy.DeclaredInteraction{
				Peer:      flow.To,
				Initiator: initiator,
				Channel:   flow.Channel,
				Carries:   flow.Carries,
				Purpose:   flow.Purpose,
				Level:     string(policy.LevelShould),
			}
			id := interactionID(label, proposal)
			if seen[id] {
				continue
			}
			seen[id] = true
			lines = append(lines, renderInteraction(proposedInteraction{
				ID:        id,
				Peer:      proposal.Peer,
				Initiator: proposal.Initiator,
				Channel:   proposal.Channel,
				Carries:   proposal.Carries,
				Purpose:   proposal.Purpose,
				Level:     proposal.Level,
			}))
		}
		if len(lines) == 0 {
			continue
		}
		proposed++
		fmt.Fprintf(builder, "\n# context: %s\ninteractions:\n", label)
		for _, line := range lines {
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}
	if proposed == 0 {
		builder.WriteString("# No observed flow maps to a declared context; nothing to propose.\n")
	}
	return builder.String()
}

// unknownTarget reports a flow whose peer is not a context or a role: pasting
// `peer: unknown` names nothing the spec can resolve, so the proposal is
// dropped rather than emitted.
func unknownTarget(peer string) bool {
	return peer == "" || peer == policy.RoleUnknown
}

type proposedInteraction struct {
	ID string `json:"id"`

	Peer string `json:"peer"`

	Initiator string `json:"initiator"`

	Channel string `json:"channel,omitempty"`

	Carries []string `json:"carries,omitempty"`

	Purpose string `json:"purpose,omitempty"`

	Level string `json:"level"`
}

// renderInteraction writes one item in the field order the CRD and the CLM
// block use, so a pasted proposal reads like the spec a person would write.
func renderInteraction(proposal proposedInteraction) string {
	builder := &strings.Builder{}
	fmt.Fprintf(builder, "  - id: %s\n", proposal.ID)
	fmt.Fprintf(builder, "    peer: %s\n", proposal.Peer)
	fmt.Fprintf(builder, "    initiator: %s\n", proposal.Initiator)
	if proposal.Channel != "" {
		fmt.Fprintf(builder, "    channel: %s\n", proposal.Channel)
	}
	if len(proposal.Carries) > 0 {
		fmt.Fprintf(builder, "    carries: [%s]\n", strings.Join(proposal.Carries, ", "))
	}
	if proposal.Purpose != "" {
		fmt.Fprintf(builder, "    purpose: %s\n", proposal.Purpose)
	}
	fmt.Fprintf(builder, "    level: %s", proposal.Level)
	return builder.String()
}

func interactionID(context string, proposal policy.DeclaredInteraction) string {
	parts := []string{context, proposal.Peer, proposal.Purpose}
	return "i." + slug(strings.Join(parts, "-"))
}

func slug(value string) string {
	lowered := strings.ToLower(value)
	builder := &strings.Builder{}
	lastDash := false
	for _, char := range lowered {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func printModel(out io.Writer, commit string, model policy.ArchitectureModel, members []policyeval.ResolvedMember) {
	fmt.Fprintf(out, "repository: %s  commit: %s\n", model.Metadata.Name, shortCommit(commit))
	for _, member := range members {
		fmt.Fprintf(out, "member: %s %s at %s\n", member.Member.Name, member.Member.Ref, shortCommit(member.Commit))
	}
	fmt.Fprintf(out, "components: %d\n", len(model.Spec.Components))
	for _, component := range model.Spec.Components {
		context := component.Context
		if context == "" {
			context = "-"
		}
		fmt.Fprintf(out, "  %-24s roles=[%s] context=%s source=%s\n",
			component.Name, strings.Join(component.Roles, ","), context, component.Source)
	}
	fmt.Fprintf(out, "effects: %d\n", len(model.Spec.Effects))
	fmt.Fprintf(out, "flows: %d\n", len(model.Spec.Flows))
	for _, flow := range model.Spec.Flows {
		fmt.Fprintf(out, "  %s -> %s  initiator=%s channel=%s carries=[%s] purpose=%s source=%s\n",
			flow.From, flow.To, flow.Initiator, emptyDash(flow.Channel),
			strings.Join(flow.Carries, ","), emptyDash(flow.Purpose), flow.Source)
		for _, evidence := range flow.Evidence {
			fmt.Fprintf(out, "      %s\n", evidence)
		}
	}
	fmt.Fprintf(out, "triggers: %d\n", len(model.Spec.Triggers))
	counts := map[string]int{}
	keys := []string{}
	for _, trigger := range model.Spec.Triggers {
		key := trigger.From + " -> " + trigger.To
		if counts[key] == 0 {
			keys = append(keys, key)
		}
		counts[key]++
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(out, "  %s\n", key)
	}
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
