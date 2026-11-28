//go:build unix

package rootdir

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive flock on f, without waiting when nowait.
func lockFile(f *os.File, nowait bool) bool {
	how := syscall.LOCK_EX
	if nowait {
		how |= syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), how) == nil
}

func unlockFile(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

// lockShared takes the shared lock on f, waiting for an exclusive holder.
func lockShared(f *os.File) bool { return syscall.Flock(int(f.Fd()), syscall.LOCK_SH) == nil }

// processAlive reports whether a process with this pid exists.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return pid > 0 && (err == nil || err == syscall.EPERM)
}
