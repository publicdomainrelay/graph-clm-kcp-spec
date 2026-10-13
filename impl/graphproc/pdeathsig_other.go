//go:build !linux

package graphproc

import "syscall"

func setPdeathsig(attributes *syscall.SysProcAttr) {
}
