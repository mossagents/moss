//go:build windows

package sandbox

import (
	"os"
	"syscall"
)

func newProcessGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func killProcessGroup(p *os.Process) {
	_ = p.Kill()
}
