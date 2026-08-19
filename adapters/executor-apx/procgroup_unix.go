//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// setProcGroup places the child in its own process group and installs a
// Cancel hook that kills the whole group, so grandchildren holding the
// stdout pipe die with the child on deadline.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == nil || errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
