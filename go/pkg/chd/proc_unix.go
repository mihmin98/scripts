//go:build unix

package chd

import (
	"os/exec"
	"syscall"
)

// detachProcess puts chdman in its own session, the equivalent of Python's
// start_new_session=True, so a Ctrl-C delivered to the terminal's foreground
// process group does not reach it.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// signalTerminate asks chdman to shut down; the caller decides how long to
// wait before killing it.
func signalTerminate(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGTERM)
}
