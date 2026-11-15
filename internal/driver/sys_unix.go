//go:build unix

package driver

import (
	"os"
	"syscall"
)

// groupAttr: the child in its own process group, so killPG ends its tree.
func groupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// sessionAttr: the child in its own session (no terminal signals).
func sessionAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

func killPG(pid int) { _ = syscall.Kill(-pid, syscall.SIGKILL) } // fails only when the group has exited

// tryLock takes an exclusive flock on f without waiting.
func tryLock(f *os.File) bool {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}
