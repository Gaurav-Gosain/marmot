//go:build !wasm

package runtime

import (
	"internal/goarch"
	"unsafe"
)

// callers walks the frame records from fp, skips skip frames and stores
// the return addresses in pc. The walk stops at a zero or non-increasing
// frame pointer (the base of a goroutine or thread stack).
func callers(fp uintptr, skip int, pc []uintptr) int {
	n := 0
	for fp != 0 && n < len(pc) {
		next := *(*uintptr)(unsafe.Pointer(fp))
		ret := *(*uintptr)(unsafe.Pointer(fp + goarch.PtrSize))
		if ret == 0 {
			break
		}
		if skip > 0 {
			skip--
		} else {
			pc[n] = ret
			n++
		}
		if next <= fp || next-fp > 1<<24 {
			break
		}
		fp = next
	}
	return n
}
