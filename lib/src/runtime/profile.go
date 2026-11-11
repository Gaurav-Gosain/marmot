// Profiling API. marmot records no profiles: the functions exist so that
// runtime/pprof and testing (which imports it) build, and they report
// empty profiles. Goroutine profile labels are kept, so pprof.Do and
// pprof.Label work within a goroutine. Compiled only by marmot.

package runtime

import (
	"internal/profilerecord"
	"unsafe"
)

// MemProfileRate controls the fraction of allocations that are recorded.
// marmot records none.
var MemProfileRate int = 512 * 1024

// StackRecord describes a single execution stack.
type StackRecord struct {
	Stack0 [32]uintptr
}

// Stack returns the stack trace associated with the record.
func (r *StackRecord) Stack() []uintptr {
	for i, v := range r.Stack0 {
		if v == 0 {
			return r.Stack0[0:i]
		}
	}
	return r.Stack0[0:]
}

// MemProfileRecord describes the live objects allocated by a particular
// call sequence.
type MemProfileRecord struct {
	AllocBytes, FreeBytes     int64
	AllocObjects, FreeObjects int64
	Stack0                    [32]uintptr
}

// InUseBytes returns the number of bytes in use.
func (r *MemProfileRecord) InUseBytes() int64 { return r.AllocBytes - r.FreeBytes }

// InUseObjects returns the number of objects in use.
func (r *MemProfileRecord) InUseObjects() int64 { return r.AllocObjects - r.FreeObjects }

// Stack returns the stack trace associated with the record.
func (r *MemProfileRecord) Stack() []uintptr {
	for i, v := range r.Stack0 {
		if v == 0 {
			return r.Stack0[0:i]
		}
	}
	return r.Stack0[0:]
}

// BlockProfileRecord describes blocking events at a call sequence.
type BlockProfileRecord struct {
	Count  int64
	Cycles int64
	StackRecord
}

// MemProfile returns a profile of memory allocated and freed. It is always
// empty.
func MemProfile(p []MemProfileRecord, inuseZero bool) (n int, ok bool) { return 0, true }

// BlockProfile returns the blocking profile. It is always empty.
func BlockProfile(p []BlockProfileRecord) (n int, ok bool) { return 0, true }

// MutexProfile returns the mutex contention profile. It is always empty.
func MutexProfile(p []BlockProfileRecord) (n int, ok bool) { return 0, true }

// ThreadCreateProfile returns the thread creation profile. It is always
// empty.
func ThreadCreateProfile(p []StackRecord) (n int, ok bool) { return 0, true }

// GoroutineProfile returns the stacks of all goroutines. marmot has no
// unwinder and reports no records.
func GoroutineProfile(p []StackRecord) (n int, ok bool) { return 0, true }

// SetCPUProfileRate sets the CPU profiling rate. marmot does not sample.
func SetCPUProfileRate(hz int) {}

//go:linkname pprof_goroutineProfileWithLabels
func pprof_goroutineProfileWithLabels(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	return 0, true
}

//go:linkname pprof_goroutineLeakProfileWithLabels
func pprof_goroutineLeakProfileWithLabels(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	return 0, true
}

//go:linkname pprof_memProfileInternal
func pprof_memProfileInternal(p []profilerecord.MemProfileRecord, inuseZero bool) (n int, ok bool) {
	return 0, true
}

//go:linkname pprof_blockProfileInternal
func pprof_blockProfileInternal(p []profilerecord.BlockProfileRecord) (n int, ok bool) {
	return 0, true
}

//go:linkname pprof_mutexProfileInternal
func pprof_mutexProfileInternal(p []profilerecord.BlockProfileRecord) (n int, ok bool) {
	return 0, true
}

//go:linkname pprof_threadCreateInternal
func pprof_threadCreateInternal(p []profilerecord.StackRecord) (n int, ok bool) {
	return 0, true
}

//go:linkname pprof_fpunwindExpand
func pprof_fpunwindExpand(dst, src []uintptr) int { return copy(dst, src) }

//go:linkname pprof_makeProfStack
func pprof_makeProfStack() []uintptr { return make([]uintptr, 32) }

//go:linkname pprof_cyclesPerSecond runtime/pprof.runtime_cyclesPerSecond
func pprof_cyclesPerSecond() int64 { return 1e9 }

// readProfile returns no CPU profile data and reports end of profile.
//
//go:linkname runtime_pprof_readProfile runtime/pprof.readProfile
func runtime_pprof_readProfile() ([]uint64, []unsafe.Pointer, bool) {
	return nil, nil, true
}

//go:linkname runtime_FrameStartLine runtime/pprof.runtime_FrameStartLine
func runtime_FrameStartLine(f *Frame) int { return f.startLine }

//go:linkname runtime_FrameSymbolName runtime/pprof.runtime_FrameSymbolName
func runtime_FrameSymbolName(f *Frame) string { return f.Function }

//go:linkname runtime_expandFinalInlineFrame runtime/pprof.runtime_expandFinalInlineFrame
func runtime_expandFinalInlineFrame(stk []uintptr) []uintptr { return stk }

//go:linkname runtime_setProfLabel runtime/pprof.runtime_setProfLabel
func runtime_setProfLabel(labels unsafe.Pointer) { getg().labels = labels }

//go:linkname runtime_getProfLabel runtime/pprof.runtime_getProfLabel
func runtime_getProfLabel() unsafe.Pointer { return getg().labels }

//go:linkname goroutineleakcount runtime/pprof.runtime_goroutineleakcount
func goroutineleakcount() int { return 0 }

//go:linkname goroutineLeakGC runtime/pprof.runtime_goroutineLeakGC
func goroutineLeakGC() {}

// Execution tracing. marmot has no tracer: StartTrace fails, and the user
// annotations of runtime/trace are dropped.

// StartTrace enables tracing. marmot does not support it.
func StartTrace() error { return errorString("tracing is not supported by marmot") }

// StopTrace stops tracing, if it was enabled.
func StopTrace() {}

// ReadTrace returns the next chunk of trace data; there is none.
func ReadTrace() []byte { return nil }

//go:linkname trace_userTaskCreate runtime/trace.userTaskCreate
func trace_userTaskCreate(id, parentID uint64, taskType string) {}

//go:linkname trace_userTaskEnd runtime/trace.userTaskEnd
func trace_userTaskEnd(id uint64) {}

//go:linkname trace_userRegion runtime/trace.userRegion
func trace_userRegion(id, mode uint64, regionType string) {}

//go:linkname trace_userLog runtime/trace.userLog
func trace_userLog(id uint64, category, message string) {}

//go:linkname traceClockUnitsPerSecond runtime/trace.runtime_traceClockUnitsPerSecond
func traceClockUnitsPerSecond() uint64 { return 1e9 }

func traceAdvance(stopTrace bool) {}

func traceClockNow() uint64 { return uint64(nanotime()) }
