//go:build unix

package main

import "syscall"

// diskFree is the space available to this user on the file system of path.
func diskFree(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}

func groupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func signalGroup(pid int, sig syscall.Signal) { _ = syscall.Kill(-pid, sig) }
