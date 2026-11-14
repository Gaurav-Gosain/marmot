//go:build !wasm

package runtime

//go:linkname libc_dladdr dladdr
func libc_dladdr(addr uintptr, info *dlInfo) int32
