// libc calls for package syscall on darwin. Package syscall calls C
// functions through syscall.syscalln with the address of the C function,
// which abi.FuncPCABI0 of a libc_X_trampoline yields under marmot.
// Compiled only by marmot.

package runtime

import (
	"internal/abi"
	"unsafe"
)

//go:linkname rt_libcall rt_libcall
func rt_libcall(fn unsafe.Pointer, args unsafe.Pointer, nargs, nfixed int32, err *int32) int

//go:linkname rt_libcall_raw rt_libcall_raw
func rt_libcall_raw(fn unsafe.Pointer, args unsafe.Pointer, nargs, nfixed int32, err *int32) int

// The variadic libSystem functions that package syscall calls. Their
// variadic arguments must be passed the C way (on the stack on arm64).
func libc_open_trampoline()
func libc_openat_trampoline()
func libc_fcntl_trampoline()
func libc_ioctl_trampoline()

//go:cgo_import_dynamic libc_open open "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_openat openat "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_fcntl fcntl "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_ioctl ioctl "/usr/lib/libSystem.B.dylib"

// fixedArgs returns the number of named parameters of the C function fn
// called with n arguments.
func fixedArgs(fn uintptr, n int) int32 {
	switch fn {
	case abi.FuncPCABI0(libc_open_trampoline), abi.FuncPCABI0(libc_fcntl_trampoline), abi.FuncPCABI0(libc_ioctl_trampoline):
		if n > 2 {
			return 2
		}
	case abi.FuncPCABI0(libc_openat_trampoline):
		if n > 3 {
			return 3
		}
	}
	return int32(n)
}

//go:linkname syscall_syscalln syscall.syscalln
func syscall_syscalln(fn uintptr, args ...uintptr) (r1, r2, err uintptr) {
	var e int32
	var p unsafe.Pointer
	if len(args) > 0 {
		p = unsafe.Pointer(&args[0])
	}
	r := rt_libcall(unsafe.Pointer(fn), p, int32(len(args)), fixedArgs(fn, len(args)), &e)
	return uintptr(r), 0, uintptr(e)
}

//go:linkname syscall_rawsyscalln syscall.rawsyscalln
func syscall_rawsyscalln(fn uintptr, args ...uintptr) (r1, r2, err uintptr) {
	var e int32
	var p unsafe.Pointer
	if len(args) > 0 {
		p = unsafe.Pointer(&args[0])
	}
	r := rt_libcall_raw(unsafe.Pointer(fn), p, int32(len(args)), fixedArgs(fn, len(args)), &e)
	return uintptr(r), 0, uintptr(e)
}

// Trap based system calls (syscall.Syscall and friends) go through the C
// core's rt_syscall, which returns -errno on failure.

func trapSyscall(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, err uintptr) {
	r := rt_syscall(num, a1, a2, a3, a4, a5, a6)
	if r < 0 && r > -4096 {
		return ^uintptr(0), 0, uintptr(-r)
	}
	return uintptr(r), 0, 0
}

//go:linkname syscall_Syscall syscall.Syscall
func syscall_Syscall(num, a1, a2, a3 uintptr) (r1, r2, err uintptr) {
	return trapSyscall(num, a1, a2, a3, 0, 0, 0)
}

//go:linkname syscall_Syscall6 syscall.Syscall6
func syscall_Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, err uintptr) {
	return trapSyscall(num, a1, a2, a3, a4, a5, a6)
}

//go:linkname syscall_RawSyscall syscall.RawSyscall
func syscall_RawSyscall(num, a1, a2, a3 uintptr) (r1, r2, err uintptr) {
	return trapSyscall(num, a1, a2, a3, 0, 0, 0)
}

//go:linkname syscall_RawSyscall6 syscall.RawSyscall6
func syscall_RawSyscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, err uintptr) {
	return trapSyscall(num, a1, a2, a3, a4, a5, a6)
}

// fcntl is used by internal/syscall/unix.
func fcntl(fd, cmd, arg int32) (ret int32, errno int32) {
	args := [3]uintptr{uintptr(fd), uintptr(cmd), uintptr(arg)}
	var e int32
	r := rt_libcall(unsafe.Pointer(abi.FuncPCABI0(libc_fcntl_trampoline)), unsafe.Pointer(&args[0]), 3, 2, &e)
	if int32(r) == -1 {
		return -1, e
	}
	return int32(r), 0
}
