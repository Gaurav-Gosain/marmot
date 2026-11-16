package runtime

import "unsafe"

//go:linkname rt_wasm_callers rt_wasm_callers
func rt_wasm_callers(pc unsafe.Pointer, max int32) int32

// callers on wasm: there are no frame records to walk. A host that reports
// the wasm frames (the browser playground, runtime/core/wasm/callers.c)
// gives their pcs, innermost first; fp is not used. The frames of the walk
// itself (rt_wasm_callers and callers) and of its caller are dropped, which
// leaves what the frame walk from that caller's frame gives natively.
// C runtime frames stay, as in the native walk (callers' skip counts
// them). Without such a host there are no frames.
func callers(fp uintptr, skip int, pc []uintptr) int {
	var buf [128]uintptr
	k := int(rt_wasm_callers(unsafe.Pointer(&buf[0]), int32(len(buf))))
	var fr [maxInlineDepth]pcInfo
	i := 0
	for j := 0; j < k; j++ {
		nf := pcFrames(buf[j]-1, fr[:])
		if nf > 0 && fr[nf-1].namePrefix == "runtime." && fr[nf-1].name == "callers" {
			i = j + 1
			break
		}
	}
	i++ // the caller of callers
	n := 0
	for ; i < k && n < len(pc); i++ {
		if skip > 0 {
			skip--
			continue
		}
		pc[n] = buf[i]
		n++
	}
	return n
}
