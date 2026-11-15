//go:build unix

package play

import (
	"os/exec"
	"syscall"
)

// inGroup runs cmd in its own process group, killed whole when its context ends.
func inGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
