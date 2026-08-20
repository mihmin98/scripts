//go:build windows

package chd

import (
	"os/exec"
	"syscall"
)

// detachProcess gives chdman its own process group, the equivalent of Python's
// CREATE_NEW_PROCESS_GROUP, so a console Ctrl-C does not reach it.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// signalTerminate stops chdman. Windows has no SIGTERM equivalent for a
// process in another group, so this is an outright kill.
func signalTerminate(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
