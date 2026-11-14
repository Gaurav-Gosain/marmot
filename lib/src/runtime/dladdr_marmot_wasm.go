package runtime

// libc_dladdr finds nothing on wasm: one module, no dynamic linker.
func libc_dladdr(addr uintptr, info *dlInfo) int32 { return 0 }
