package main

import (
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"

	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/persist"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"
)

func runUp(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl up", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "the cloned repository to work on; its git top level is the repository root")
	repository := fs.String("repository", "", "Repository name and kcp workspace; default is the directory name")
	out := fs.String("out", "", "write the session (kcp url and port, kine port, kubeconfig, workspace, pids) as JSON to this file")
	summarize := fs.Bool("summarize", true, "summarize every context into a spec with the model after indexing")
	agent := fs.String("agent", os.Getenv("SPECD_AGENT"), "agent kind specd runs (claude, claude-mod, pi, scripted:<file>); empty is specd's default")
	remote := fs.String("remote", "origin", "git remote an existing open-architecture branch is fetched from; empty fetches nothing")
	push := fs.Bool("push", false, "push the open-architecture branch to --remote after every commit")
	specdPath := fs.String("specd", defaultSibling("SPECD_BIN", "specd"), "specd binary")
	clmMod := fs.String("clm-mod", defaultClmMod(), "cc-clm-mod plugin folder the model runs with")
	noSpecd := fs.Bool("no-specd", false, "start kcp and register the repository, but do not start specd")
	stopOthers := fs.Bool("stop-others", false, "also stop the kcp and kine of the branches this checkout left; their specd stops either way")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	dir, err := repoDir(*repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl up: %v\n", err)
		return exitError
	}
	top, err := session.TopLevel(dir)
	if err != nil {
		fmt.Fprintf(stderr, "specctl up: %v\n", err)
		return exitError
	}
	branch, err := currentBranch(top)
	if err != nil {
		fmt.Fprintf(stderr, "specctl up: %v\n", err)
		return exitError
	}
	name := *repository
	if name == "" {
		name = repositoryNameFor(top)
	}
	record := session.Record{
		Repo:                top,
		Repository:          name,
		Branch:              branch,
		Workspace:           "root:" + name,
		Namespace:           options.namespace,
		KcpRoot:             session.KcpRoot(top, branch),
		ClmMod:              *clmMod,
		ClmDocDir:           statedir.ClmDocDir(),
		AdminKubeconfig:     filepath.Join(session.KcpRoot(top, branch), "admin.kubeconfig"),
		WorkspaceKubeconfig: filepath.Join(session.KcpRoot(top, branch), name+".kubeconfig"),
	}
	previous, hadPrevious, _ := session.Load(top, branch, session.DefaultBranch(top))
	if hadPrevious && session.Alive(previous.SpecdPid, "specd") {
		record.SpecdPid = previous.SpecdPid
		record.SpecdLog = previous.SpecdLog
	}

	fmt.Fprintf(stdout, "repository %s at %s on branch %s\n", name, top, branch)
	if err := startKcp(ctx, &record, previous, hadPrevious, stdout); err != nil {
		fmt.Fprintf(stderr, "specctl up: %v\n", err)
		return exitError
	}
	client, err := kcpclient.New(kcpclient.Options{
		Kubeconfig: record.AdminKubeconfig,
		Workspace:  record.Workspace,
		Namespace:  record.Namespace,
		QPS:        float32(options.qps),
		Burst:      options.burst,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl up: %v\n", err)
		return exitError
	}

	_, getErr := client.Get(ctx, specapi.RepositoryGVR, record.Namespace, name)
	switch {
	case getErr == nil:
		fmt.Fprintf(stdout, "kcp already holds Repository %s in %s\n", name, record.Workspace)
	case kcpclient.IsNotFound(getErr):
		restored, err := persist.Restore(ctx, persist.RestoreOptions{Cluster: client, Namespace: record.Namespace, Repository: name, RepoPath: top, Remote: *remote, CodeBranch: branch})
		switch {
		case err == nil:
			from := "the local branch"
			if restored.Fetched {
				from = "the branch on " + *remote
			}
			fmt.Fprintf(stdout, "restored %d context(s) from %s (%s at %s); specd re-indexes the code\n", len(restored.Contexts), from, restored.Branch, short(restored.Commit))
		case errors.Is(err, persist.ErrNoBranch):
			if err := applyRepository(ctx, client, record, *summarize); err != nil {
				fmt.Fprintf(stderr, "specctl up: %v\n", err)
				return exitError
			}
			fmt.Fprintf(stdout, "no open-architecture/%s branch yet: applied Repository %s; specd indexes %s and builds the specs\n", name, name, top)
		default:
			fmt.Fprintf(stderr, "specctl up: restore: %v\n", err)
			return exitError
		}
	default:
		fmt.Fprintf(stderr, "specctl up: %v\n", getErr)
		return exitError
	}

	stopOtherBranchSpecds(record, *stopOthers, stdout)

	if !*noSpecd && !session.Alive(record.SpecdPid, "specd") {
		pushRemote := ""
		if *push {
			pushRemote = *remote
		}
		pid, log, err := startSpecd(record, *specdPath, *agent, pushRemote)
		if err != nil {
			fmt.Fprintf(stderr, "specctl up: start specd: %v\n", err)
			return exitError
		}
		record.SpecdPid = pid
		record.SpecdLog = log
		fmt.Fprintf(stdout, "specd started, pid %d, log %s\n", pid, log)
	} else if record.SpecdPid > 0 {
		fmt.Fprintf(stdout, "specd already running, pid %d\n", record.SpecdPid)
	}
	if err := session.Save(record); err != nil {
		fmt.Fprintf(stderr, "specctl up: %v\n", err)
		return exitError
	}
	if err := writeOut(*out, record); err != nil {
		fmt.Fprintf(stderr, "specctl up: write %s: %v\n", *out, err)
		return exitError
	}
	printNextSteps(stdout, record)
	return exitOK
}

func startKcp(ctx context.Context, record *session.Record, previous session.Record, hadPrevious bool, stdout io.Writer) error {
	instance := kcpproc.Instance{}
	if hadPrevious {
		instance = instanceOf(previous)
	}
	reused := hadPrevious && kcpproc.Ready(instance)
	if !reused {
		if probed, ok := kcpproc.Probe(record.KcpRoot); ok {
			instance = probed
			reused = true
		}
	}
	if !reused {
		started, err := kcpproc.Start(ctx, kcpproc.Options{Root: record.KcpRoot})
		if err != nil {
			return err
		}
		instance = started
	}
	record.KcpRoot = instance.Root
	record.KcpPort, record.KcpURL, record.KcpPid = instance.KcpPort, instance.KcpURL, instance.KcpPid
	record.KinePort, record.KineURL, record.KinePid = instance.KinePort, instance.KineURL, instance.KinePid
	record.AdminKubeconfig = instance.AdminKubeconfig
	record.WorkspaceKubeconfig = filepath.Join(instance.Root, record.Repository+".kubeconfig")

	deployDir, err := session.ExtractDeploy()
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "bash", filepath.Join(deployDir, "install-specs.sh"))
	command.Env = append(os.Environ(),
		"ROOT="+record.KcpRoot,
		"KUBECONFIG_PATH="+record.AdminKubeconfig,
		"SPECS_WORKSPACE="+record.Repository,
		"SPECS_NAMESPACE="+record.Namespace,
		"WORKSPACE_KUBECONFIG="+record.WorkspaceKubeconfig,
	)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("install-specs.sh: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	state := "started"
	if reused {
		state = "already running"
	}
	fmt.Fprintf(stdout, "kcp %s at %s (kernel-assigned port %d, kine port %d, state %s), workspace %s\n", state, record.KcpURL, record.KcpPort, record.KinePort, record.KcpRoot, record.Workspace)
	return nil
}

func instanceOf(record session.Record) kcpproc.Instance {
	return kcpproc.Instance{
		Root:            record.KcpRoot,
		KcpPort:         record.KcpPort,
		KcpPid:          record.KcpPid,
		KcpURL:          record.KcpURL,
		KinePort:        record.KinePort,
		KinePid:         record.KinePid,
		KineURL:         record.KineURL,
		AdminKubeconfig: record.AdminKubeconfig,
	}
}

func writeOut(path string, record session.Record) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func applyRepository(ctx context.Context, client *kcpclient.Client, record session.Record, summarize bool) error {
	branch, err := currentBranch(record.Repo)
	if err != nil {
		return err
	}
	repository := spec.Repository{}
	repository.APIVersion = specapi.Group + "/" + specapi.Version
	repository.Kind = specapi.RepositoryKind
	repository.Name = record.Repository
	repository.Namespace = record.Namespace
	repository.Spec.Source = &spec.RepositorySource{Path: record.Repo}
	repository.Spec.Branch = branch
	repository.Spec.Verify = detectVerify(record.Repo)
	repository.Spec.Populate = &spec.RepositoryPopulate{
		Partition: spec.PartitionDirectory,
		Summarize: summarize,
		Root:      true,
	}
	object, err := kcpclient.Unstructured(&repository)
	if err != nil {
		return err
	}
	_, err = client.Apply(ctx, object)
	return err
}

func currentBranch(repo string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("%s has a detached HEAD; check out the branch to work on first", repo)
	}
	return strings.TrimSpace(string(out)), nil
}

func detectVerify(repo string) []string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(repo, name))
		return err == nil
	}
	switch {
	case exists("go.mod"):
		return []string{"go", "test", "./..."}
	case exists("deno.json"), exists("deno.jsonc"):
		return []string{"deno", "test"}
	case exists("package.json"):
		return []string{"npm", "test"}
	case exists("Cargo.toml"):
		return []string{"cargo", "test"}
	case exists("pyproject.toml"), exists("setup.py"):
		return []string{"python3", "-m", "pytest"}
	}
	return nil
}

func startSpecd(record session.Record, specdPath, agent, pushRemote string) (int, string, error) {
	logDir := filepath.Join(statedir.Dir(), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return 0, "", err
	}
	logPath := filepath.Join(logDir, filepath.Base(session.RepoDir(record.Repo))+"--"+session.BranchSlug(record.Branch)+".specd.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, "", err
	}
	defer logFile.Close()
	args := []string{
		"--kubeconfig", record.AdminKubeconfig,
		"--workspace", record.Workspace,
		"--namespace", record.Namespace,
		"--cache-dir", filepath.Join(statedir.Dir(), "cache"),
		"--log-dir", logDir,
	}
	if agent != "" {
		args = append(args, "--agent", agent)
	}
	if record.ClmMod != "" {
		args = append(args, "--clm-mod", record.ClmMod)
	}
	if pushRemote != "" {
		args = append(args, "--persist-remote", pushRemote)
	}
	command := exec.Command(specdPath, args...)
	command.Dir = record.Repo
	command.Stdout = logFile
	command.Stderr = logFile
	command.Env = append(os.Environ(), "SPECD_CLM_DOC_DIR="+record.ClmDocDir)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return 0, "", err
	}
	pid := command.Process.Pid
	if err := command.Process.Release(); err != nil {
		return 0, "", err
	}
	time.Sleep(500 * time.Millisecond)
	if !session.Alive(pid, "specd") {
		return 0, logPath, fmt.Errorf("specd exited at once; see %s", logPath)
	}
	return pid, logPath, nil
}

func stopOtherBranchSpecds(current session.Record, stopKcp bool, stdout io.Writer) {
	records, err := session.List()
	if err != nil {
		fmt.Fprintf(stdout, "could not list this machine's instances: %v\n", err)
		return
	}
	for _, other := range records {
		if other.Repo != current.Repo || other.Branch == current.Branch {
			continue
		}
		if session.Alive(other.SpecdPid, "specd") {
			stopSpecdProcess(other.SpecdPid)
			other.SpecdPid = 0
			other.SpecdLog = ""
			if err := session.Save(other); err != nil {
				fmt.Fprintf(stdout, "could not record the stopped specd of branch %s: %v\n", other.Branch, err)
			}
			fmt.Fprintf(stdout, "specd of branch %s stopped (the checkout is on %s)\n", other.Branch, current.Branch)
		}
		if stopKcp && kcpproc.Ready(instanceOf(other)) {
			kcpproc.Stop(instanceOf(other))
			fmt.Fprintf(stdout, "kcp of branch %s stopped at %s\n", other.Branch, other.KcpURL)
		}
	}
}

func stopSpecdProcess(pid int) {
	if process, err := os.FindProcess(pid); err == nil {
		_ = process.Signal(syscall.SIGTERM)
	}
	for range 50 {
		if !session.Alive(pid, "specd") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func printNextSteps(stdout io.Writer, record session.Record) {
	fmt.Fprintf(stdout, "\nnext:\n")
	fmt.Fprintf(stdout, "  specctl status                      # populate progress, contexts, the open-architecture branch\n")
	fmt.Fprintf(stdout, "  specctl arch outline                # the architecture kcp holds for this repository\n")
	fmt.Fprintf(stdout, "  KUBECONFIG=%s kubectl --server=$(specctl env -o server) get systemcontexts\n", record.AdminKubeconfig)
	if record.ClmMod != "" {
		fmt.Fprintf(stdout, "  claude --plugin-dir %s           # the agent inspects and edits the arch through kcp\n", record.ClmMod)
	}
	fmt.Fprintf(stdout, "  git log --oneline 'open-architecture/%s*'   # every change of kcp, outside the project tree\n", record.Repository)
	fmt.Fprintf(stdout, "  specctl env -o json                 # this instance's kcp url, ports, kubeconfig, workspace\n")
	fmt.Fprintf(stdout, "  specctl down                        # stop specd and this repository's kcp\n")
}

func runDown(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl down", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "the repository specctl up was run in")
	keepKcp := fs.Bool("keep-kcp", false, "leave this repository's kcp and kine running")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dir, err := repoDir(*repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl down: %v\n", err)
		return exitError
	}
	top, err := session.TopLevel(dir)
	if err != nil {
		fmt.Fprintf(stderr, "specctl down: %v\n", err)
		return exitError
	}
	branch, err := session.CurrentBranch(top)
	if err != nil {
		fmt.Fprintf(stderr, "specctl down: %v\n", err)
		return exitError
	}
	record, ok, err := session.Load(top, branch, session.DefaultBranch(top))
	if err != nil {
		fmt.Fprintf(stderr, "specctl down: %v\n", err)
		return exitError
	}
	if !ok {
		fmt.Fprintf(stdout, "no session for %s on branch %s\n", top, branch)
		return exitOK
	}
	if session.Alive(record.SpecdPid, "specd") {
		stopSpecdProcess(record.SpecdPid)
		fmt.Fprintf(stdout, "specd %d stopped\n", record.SpecdPid)
	}
	if !*keepKcp {
		kcpproc.Stop(instanceOf(record))
		if live := kcpproc.Running(record.KcpRoot); len(live) > 0 {
			fmt.Fprintf(stderr, "specctl down: kcp %s still has process(es) %v alive under %s; the session is kept\n", record.KcpURL, live, record.KcpRoot)
			return exitError
		}
		fmt.Fprintf(stdout, "kcp %s and kine stopped\n", record.KcpURL)
	}
	if err := session.Remove(record.Repo, record.Branch); err != nil {
		fmt.Fprintf(stderr, "specctl down: %v\n", err)
		return exitError
	}
	return exitOK
}

func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "the repository specctl up was run in")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	record, code := sessionFor(*repo, stderr, "status")
	if code != exitOK {
		return code
	}
	ctx := context.Background()
	client, err := kcpclient.New(kcpclient.Options{Kubeconfig: record.AdminKubeconfig, Workspace: record.Workspace, Namespace: record.Namespace, QPS: 50, Burst: 100})
	if err != nil {
		fmt.Fprintf(stderr, "specctl status: %v\n", err)
		return exitError
	}
	specdState := "stopped"
	if session.Alive(record.SpecdPid, "specd") {
		specdState = fmt.Sprintf("running, pid %d, log %s", record.SpecdPid, record.SpecdLog)
	}
	kcpState := "stopped"
	if kcpproc.Ready(instanceOf(record)) {
		kcpState = "ready"
	}
	fmt.Fprintf(stdout, "repository  %s at %s on branch %s\nkcp         %s %s (kine %s), workspace %s\nspecd       %s\n", record.Repository, record.Repo, record.Branch, record.KcpURL, kcpState, record.KineURL, record.Workspace, specdState)
	object, err := client.Get(ctx, specapi.RepositoryGVR, record.Namespace, record.Repository)
	if err != nil {
		fmt.Fprintf(stderr, "specctl status: %v\n", err)
		return exitError
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		fmt.Fprintf(stderr, "specctl status: %v\n", err)
		return exitError
	}
	repository := typed.(*spec.Repository)
	counts := spec.PopulateCounts{}
	if repository.Status.Contexts != nil {
		counts = *repository.Status.Contexts
	}
	fmt.Fprintf(stdout, "phase       %s (%d contexts, %d summarized, %d failed)\n", valueOr(repository.Status.Phase, "Pending"), counts.Total, counts.Summarized, counts.Failed)
	store := oagit.Store{Repo: record.Repo}
	archBranch := oabranch.BranchFor(record.Repository, repository.Spec.Branch, store.DefaultBranch(ctx))
	if status := repository.Status.OpenArchitecture; status != nil && status.Branch != "" {
		archBranch = status.Branch
	}
	tip, _ := store.Tip(ctx, "refs/heads/"+archBranch)
	fmt.Fprintf(stdout, "branch      %s at %s\n", archBranch, short(tip))
	if status := repository.Status.OpenArchitecture; status != nil && len(status.Conflicts) > 0 {
		fmt.Fprintf(stdout, "conflicts   %s\n", strings.Join(status.Conflicts, "; "))
	}
	changes, err := client.List(ctx, specapi.SpecChangeGVR, record.Namespace)
	if err == nil {
		open := 0
		for index := range changes.Items {
			phase, _, _ := unstructuredString(changes.Items[index].Object, "status", "phase")
			if phase != specapi.PhaseSucceeded && phase != "Failed" {
				open++
			}
		}
		fmt.Fprintf(stdout, "changes     %d total, %d open\n", len(changes.Items), open)
	}
	return exitOK
}

func runEnv(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl env", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "the repository specctl up was run in")
	output := fs.String("o", "sh", "sh (export lines), json, or server (the workspace URL)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	record, code := sessionFor(*repo, stderr, "env")
	if code != exitOK {
		return code
	}
	switch *output {
	case "json":
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl env: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(data))
	case "server":
		server, err := kcpclient.RestConfig(record.AdminKubeconfig, "")
		if err != nil {
			fmt.Fprintf(stderr, "specctl env: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, kcpclient.WorkspaceHost(server.Host, record.Workspace))
	case "sh":
		for _, pair := range [][2]string{
			{"SPECD_KUBECONFIG", record.AdminKubeconfig},
			{"SPECD_WORKSPACE", record.Workspace},
			{"SPECD_NAMESPACE", record.Namespace},
			{"SPECD_CLM_REPO", record.Repo},
			{"SPECD_CLM_REPOSITORY", record.Repository},
			{"SPECD_CLM_DOC_DIR", record.ClmDocDir},
		} {
			fmt.Fprintf(stdout, "export %s=%q\n", pair[0], pair[1])
		}
	default:
		fmt.Fprintf(stderr, "specctl env: unknown output %q\n", *output)
		return exitUsage
	}
	return exitOK
}

func sessionFor(repo string, stderr io.Writer, command string) (session.Record, int) {
	dir, err := repoDir(repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl %s: %v\n", command, err)
		return session.Record{}, exitError
	}
	record, ok := session.ForDir(dir)
	if !ok {
		fmt.Fprintf(stderr, "specctl %s: no session for %s; run specctl up there first\n", command, dir)
		return session.Record{}, exitError
	}
	return record, exitOK
}

func unstructuredString(object map[string]any, fields ...string) (string, bool, error) {
	current := any(object)
	for _, field := range fields {
		values, ok := current.(map[string]any)
		if !ok {
			return "", false, nil
		}
		current, ok = values[field]
		if !ok {
			return "", false, nil
		}
	}
	value, ok := current.(string)
	return value, ok, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultSibling(env, name string) string {
	if value := os.Getenv(env); value != "" {
		return value
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return name
}

func defaultClmMod() string {
	if value := os.Getenv("SPECD_CLM_MOD"); value != "" {
		return value
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "..", "cc-clm-mod")
		if _, err := os.Stat(filepath.Join(candidate, ".claude-plugin", "plugin.json")); err == nil {
			if absolute, err := filepath.Abs(candidate); err == nil {
				return absolute
			}
		}
	}
	return ""
}

func repositoryNameFor(top string) string {
	out, err := exec.Command("git", "-C", top, "remote", "get-url", "origin").Output()
	if err == nil {
		url := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(string(out)), "/"), ".git")
		if index := strings.LastIndexAny(url, "/:"); index >= 0 {
			url = url[index+1:]
		}
		if name := ingest.SanitizeName(url); name != "" {
			return name
		}
	}
	return ingest.SanitizeName(filepath.Base(top))
}
