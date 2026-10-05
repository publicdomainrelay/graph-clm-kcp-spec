package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"
)

func startKeeper(root string) (int, error) {
	dieWith := kcpproc.DieWithPid()
	if dieWith <= 0 {
		return 0, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(filepath.Join(root, "keeper.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	command := exec.Command(executable, "keeper", "--root", root, "--die-with", strconv.Itoa(dieWith))
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return 0, err
	}
	pid := command.Process.Pid
	if err := command.Process.Release(); err != nil {
		return 0, err
	}
	return pid, nil
}

func runKeeper(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl keeper", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "the kcp root whose kcp, kine and specd die with the watched process")
	dieWith := fs.Int("die-with", 0, "the process whose death ends the kcp, kine and specd under --root; the id in "+kcpproc.DieWithEnv)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *root == "" || *dieWith <= 1 {
		fmt.Fprintf(stderr, "specctl keeper: --root and --die-with are required\n")
		return exitUsage
	}
	kcpproc.Watch(*root, *dieWith)
	return exitOK
}
