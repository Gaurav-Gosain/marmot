// Copyright 2026 The marmot Authors. Race detector, Go side (marmot build
// -race): the functions gc's runtime pushes into internal/race, and the
// symbolizer the race runtime (runtime/core/src/race.c) calls for reports.

//go:build race

package runtime

import "unsafe"

const raceenabled = true

// A pc of 0 below asks the C side for the caller of the internal/race
// function (__builtin_return_address(1); Go frames keep frame records).

//go:linkname rt_race_acquire rt_race_acquire
func rt_race_acquire(addr unsafe.Pointer)

//go:linkname rt_race_release rt_race_release
func rt_race_release(addr unsafe.Pointer)

//go:linkname rt_race_release_merge rt_race_release_merge
func rt_race_release_merge(addr unsafe.Pointer)

//go:linkname rt_race_release_acquire rt_race_release_acquire
func rt_race_release_acquire(addr unsafe.Pointer)

// Channel synchronization (chan.go, chanring.go, select.go), as gc models
// it. A goroutine that parks on an unbuffered channel or in a select
// releases on its sudog; the goroutine that completes the exchange
// acquires and releases on that sudog (raceReleaseAcquire); the parked one
// acquires on it when it wakes. So both sides of an exchange happen before
// the other's next step. A buffered channel synchronizes on the slot: send
// N happens before receive N, and receive N before send N+cap. close
// releases on the channel, and a receive that sees the close acquires.
func raceAcquire(addr unsafe.Pointer)        { rt_race_acquire(addr) }
func raceRelease(addr unsafe.Pointer)        { rt_race_release(addr) }
func raceReleaseMerge(addr unsafe.Pointer)   { rt_race_release_merge(addr) }
func raceReleaseAcquire(addr unsafe.Pointer) { rt_race_release_acquire(addr) }

// Accesses the runtime makes on the program's behalf, as gc's racereadpc
// and friends (map headers, channels, slice and string data).
func raceReadAddr(p unsafe.Pointer)              { rt_race_read(p, 0) }
func raceWriteAddr(p unsafe.Pointer)             { rt_race_write(p, 0) }
func raceReadRange(p unsafe.Pointer, n uintptr)  { rt_race_read_range(p, n, 0) }
func raceWriteRange(p unsafe.Pointer, n uintptr) { rt_race_write_range(p, n, 0) }

// raceFingo: everything before the collection that found the object happens
// before its finalizer and cleanups, as gc's racefingo.
func raceFingo() { rt_race_fingo() }

// raceSkip(1) and raceSkip(-1) bracket runtime code on this thread whose
// annotated operations are not the program's (finTable under finLock).
func raceSkip(d int32) { rt_race_skip(d) }

//go:linkname rt_race_skip rt_race_skip
func rt_race_skip(d int32)

// raceReset: the object at p starts a new life without an allocation.
func raceReset(p unsafe.Pointer, n uintptr) { rt_race_reset(p, n) }

//go:linkname rt_race_reset rt_race_reset
func rt_race_reset(p unsafe.Pointer, n uintptr)

//go:linkname rt_race_fingo rt_race_fingo
func rt_race_fingo()

//go:linkname rt_race_read rt_race_read
func rt_race_read(addr unsafe.Pointer, pc uintptr)

//go:linkname rt_race_write rt_race_write
func rt_race_write(addr unsafe.Pointer, pc uintptr)

//go:linkname rt_race_read_range rt_race_read_range
func rt_race_read_range(addr unsafe.Pointer, n uintptr, pc uintptr)

//go:linkname rt_race_write_range rt_race_write_range
func rt_race_write_range(addr unsafe.Pointer, n uintptr, pc uintptr)

//go:linkname rt_race_ignore rt_race_ignore
func rt_race_ignore(begin int32)

//go:linkname rt_race_errors rt_race_errors
func rt_race_errors() int

//go:linkname race_Acquire internal/race.Acquire
func race_Acquire(addr unsafe.Pointer) { rt_race_acquire(addr) }

//go:linkname race_Release internal/race.Release
func race_Release(addr unsafe.Pointer) { rt_race_release(addr) }

//go:linkname race_ReleaseMerge internal/race.ReleaseMerge
func race_ReleaseMerge(addr unsafe.Pointer) { rt_race_release_merge(addr) }

//go:linkname race_Disable internal/race.Disable
func race_Disable() { rt_race_ignore(1) }

//go:linkname race_Enable internal/race.Enable
func race_Enable() { rt_race_ignore(0) }

//go:linkname race_Read internal/race.Read
func race_Read(addr unsafe.Pointer) { rt_race_read(addr, 0) }

//go:linkname race_ReadPC internal/race.ReadPC
func race_ReadPC(addr unsafe.Pointer, callerpc, pc uintptr) { rt_race_read(addr, pc) }

//go:linkname race_ReadObjectPC internal/race.ReadObjectPC
func race_ReadObjectPC(t *_type, addr unsafe.Pointer, callerpc, pc uintptr) {
	rt_race_read_range(addr, t.size, pc)
}

//go:linkname race_Write internal/race.Write
func race_Write(addr unsafe.Pointer) { rt_race_write(addr, 0) }

//go:linkname race_WritePC internal/race.WritePC
func race_WritePC(addr unsafe.Pointer, callerpc, pc uintptr) { rt_race_write(addr, pc) }

//go:linkname race_WriteObjectPC internal/race.WriteObjectPC
func race_WriteObjectPC(t *_type, addr unsafe.Pointer, callerpc, pc uintptr) {
	rt_race_write_range(addr, t.size, pc)
}

//go:linkname race_ReadRange internal/race.ReadRange
func race_ReadRange(addr unsafe.Pointer, n int) { rt_race_read_range(addr, uintptr(n), 0) }

//go:linkname race_WriteRange internal/race.WriteRange
func race_WriteRange(addr unsafe.Pointer, n int) { rt_race_write_range(addr, uintptr(n), 0) }

//go:linkname race_Errors internal/race.Errors
func race_Errors() int { return rt_race_errors() }

// The exported race API of gc's runtime (race builds only).

func RaceRead(addr unsafe.Pointer)                { rt_race_read(addr, 0) }
func RaceWrite(addr unsafe.Pointer)               { rt_race_write(addr, 0) }
func RaceReadRange(addr unsafe.Pointer, len int)  { rt_race_read_range(addr, uintptr(len), 0) }
func RaceWriteRange(addr unsafe.Pointer, len int) { rt_race_write_range(addr, uintptr(len), 0) }
func RaceErrors() int                             { return rt_race_errors() }
func RaceAcquire(addr unsafe.Pointer)             { rt_race_acquire(addr) }
func RaceRelease(addr unsafe.Pointer)             { rt_race_release(addr) }
func RaceReleaseMerge(addr unsafe.Pointer)        { rt_race_release_merge(addr) }
func RaceDisable()                                { rt_race_ignore(1) }
func RaceEnable()                                 { rt_race_ignore(0) }

// raceSymbolize answers the race runtime's code symbolization: function,
// file and line of pc copied into the C buffers, NUL terminated. It must
// not allocate (it runs inside a TSan report, which holds TSan's locks), so
// it copies from the pc table's strings.
//
//go:linkname raceSymbolize runtime_race_symbolize
func raceSymbolize(pc uintptr, fn *byte, fncap int, file *byte, filecap int, line *int) bool {
	info, ok := pcLookup(pc)
	if !ok {
		return false
	}
	cput(fn, fncap, info.namePrefix, info.name)
	cput(file, filecap, info.filePrefix, info.file)
	*line = info.line
	return true
}

// cput writes a+b and a NUL into the n-byte buffer at p, truncated to fit.
func cput(p *byte, n int, a, b string) {
	buf := unsafe.Slice(p, n)
	k := copy(buf[:n-1], a)
	k += copy(buf[k:n-1], b)
	buf[k] = 0
}

// The pc table is read on its first use; read it now, outside any report.
func init() { pcLookup(1) }
