//go:build linux

package kcpproc

import "syscall"

func setPdeathsig(attributes *syscall.SysProcAttr) {
	attributes.Pdeathsig = syscall.SIGKILL
}
