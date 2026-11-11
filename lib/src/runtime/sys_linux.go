// System call glue for package syscall on linux. Compiled only by marmot.
// Untested on hardware here; cross-checked by the linux worker.

package runtime

import "unsafe"

// entersyscall and exitsyscall are pulled by package syscall
// (runtime_entersyscall) around blocking system calls.
func entersyscall() { rt_entersyscall() }
func exitsyscall()  { rt_exitsyscall() }

//go:linkname syscall_rawSyscallNoError syscall.rawSyscallNoError
func syscall_rawSyscallNoError(trap, a1, a2, a3 uintptr) (r1, r2 uintptr) {
	return uintptr(rt_syscall(trap, a1, a2, a3, 0, 0, 0)), 0
}

const (
	_CLONE_VM    = 0x100
	_CLONE_VFORK = 0x4000
)

// syscall_rawVforkSyscall runs clone/clone3 for os/exec. A vfork child
// shares the parent's stack, which is only safe for gc's hand written
// assembly, so marmot drops CLONE_VM and CLONE_VFORK and forks: the child
// runs forkAndExecInChild1 in its own copy of the address space.
//
//go:linkname syscall_rawVforkSyscall syscall.rawVforkSyscall
func syscall_rawVforkSyscall(trap, a1, a2, a3 uintptr) (r1 uintptr, err uintptr) {
	if trap == _SYS_clone3 {
		flags := (*uint64)(unsafe.Pointer(a1)) // clone_args.flags is the first field
		*flags &^= _CLONE_VM | _CLONE_VFORK
	} else {
		a1 &^= _CLONE_VM | _CLONE_VFORK
	}
	r := rt_syscall(trap, a1, a2, a3, 0, 0, 0)
	if r < 0 && r > -4096 {
		return ^uintptr(0), uintptr(-r)
	}
	return uintptr(r), 0
}

// fcntl is pulled by internal/syscall/unix.
func fcntl(fd, cmd, arg int32) (ret int32, errno int32) {
	r := rt_syscall(_SYS_fcntl, uintptr(fd), uintptr(cmd), uintptr(arg), 0, 0, 0)
	if r < 0 && r > -4096 {
		return -1, int32(-r)
	}
	return int32(r), 0
}

// vgetrandom is pulled by internal/syscall/unix; marmot has no vDSO
// getrandom, so callers fall back to the getrandom system call.
func vgetrandom(p []byte, flags uint32) (ret int, supported bool) {
	return -1, false
}

// getAuxv is pulled by golang.org/x/sys/cpu; marmot does not keep the
// auxiliary vector.
func getAuxv() []uintptr { return nil }

// os probes pidfd support with a syscall that may raise SIGSYS under a
// seccomp filter and brackets the probe with these. marmot installs no
// SIGSYS handler, so the default action applies and nothing changes.

//go:linkname ignoreSIGSYS os.ignoreSIGSYS
func ignoreSIGSYS() {}

//go:linkname restoreSIGSYS os.restoreSIGSYS
func restoreSIGSYS() {}
