// Copyright 2026 The marmot Authors. Without -race there is no race
// detector: the hooks behind raceenabled are dead code.

//go:build !race

package runtime

import "unsafe"

const raceenabled = false

func raceAcquire(addr unsafe.Pointer)            {}
func raceRelease(addr unsafe.Pointer)            {}
func raceReleaseMerge(addr unsafe.Pointer)       {}
func raceReleaseAcquire(addr unsafe.Pointer)     {}
func raceFingo()                                 {}
func raceSkip(d int32)                           {}
func raceReset(p unsafe.Pointer, n uintptr)      {}
func raceReadAddr(p unsafe.Pointer)              {}
func raceWriteAddr(p unsafe.Pointer)             {}
func raceReadRange(p unsafe.Pointer, n uintptr)  {}
func raceWriteRange(p unsafe.Pointer, n uintptr) {}
