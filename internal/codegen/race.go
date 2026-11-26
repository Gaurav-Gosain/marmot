package codegen

import (
	"go/token"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// raceNoInstrument are the packages whose memory accesses -race leaves
// alone: gc's runtimePkgs and extraNoInstrumentPkgs (cmd/internal/objabi,
// pkgspecial.go of go1.27), with internal/bytealg instrumented as gc does,
// plus marmot's runtime-only internal/rtabi. Packages under
// internal/runtime/ and runtime/internal/ are runtime packages too.
//
// The bodies of sync, sync/atomic and internal/sync are not instrumented
// either (gc's NoRacePkgs: walk's instrument skips them): they synchronize
// through annotations and atomics, and their plain reads of lock words
// would be reported. Their atomics still reach the race runtime: LLVM's
// pass instruments atomic operations in every function, with or without
// sanitize_thread, as gc's sync/atomic assembly calls the race runtime.
var raceNoInstrument = map[string]bool{
	"sync": true, "sync/atomic": true, "internal/sync": true,
	"runtime": true, "runtime/race": true, "runtime/msan": true, "runtime/asan": true,
	"internal/abi": true, "internal/byteorder": true, "internal/chacha8rand": true,
	"internal/coverage/rtcov": true, "internal/cpu": true, "internal/goarch": true,
	"internal/godebugs": true, "internal/goexperiment": true, "internal/goos": true,
	"internal/profilerecord": true, "internal/strconv": true, "internal/stringslite": true,
	"internal/rtabi": true,
}

// raceInstrumented reports whether -race instruments fn's memory accesses.
// gc decides by package. An instance of a generic function counts as its
// origin's package (gc uses the package that compiles the instance): an
// atomic.Pointer[T] method stays as instrumented as sync/atomic, wherever
// it is instantiated. A wrapper or thunk without a package counts as the
// package of the method it calls.
func raceInstrumented(fn *ssa.Function) bool {
	if o := fn.Origin(); o != nil {
		fn = o
	}
	var path string
	switch {
	case fn.Pkg != nil && fn.Pkg.Pkg != nil:
		path = fn.Pkg.Pkg.Path()
	case fn.Object() != nil && fn.Object().Pkg() != nil:
		path = fn.Object().Pkg().Path()
	default:
		return false // synthetic functions of no package
	}
	if raceNoInstrument[path] {
		return false
	}
	return !strings.HasPrefix(path, "runtime/internal/") && !strings.HasPrefix(path, "internal/runtime/")
}

// raceAtomicsOnly reports whether -race keeps only the atomic operations of
// fn: sync, sync/atomic and internal/sync, whose atomics are the
// synchronization the race runtime must see (gc's sync/atomic calls the race
// runtime). The runtime's own atomics are not reported, as in gc, where the
// runtime is not instrumented: an atomic load of a channel position would
// otherwise order accesses that gc treats as racing.
func raceAtomicsOnly(fn *ssa.Function) bool {
	if o := fn.Origin(); o != nil {
		fn = o
	}
	if fn.Pkg == nil || fn.Pkg.Pkg == nil {
		return false
	}
	switch fn.Pkg.Pkg.Path() {
	case "sync", "sync/atomic", "internal/sync":
		return true
	}
	return false
}

// raceSelfStore reports whether st stores back the value just loaded from
// the same address, with nothing in between that can write memory: the
// x = x a return with named results makes (return a, 10 when a is a named
// result). gc does not emit that store (issue 4014), so under -race neither
// does marmot: an instrumented write there races with a goroutine that only
// reads a.
func raceSelfStore(st *ssa.Store) bool {
	ld, ok := st.Val.(*ssa.UnOp)
	if !ok || ld.Op != token.MUL || ld.X != st.Addr || ld.Block() != st.Block() {
		return false
	}
	seen := false
	for _, in := range st.Block().Instrs {
		if in == ssa.Instruction(ld) {
			seen = true
			continue
		}
		if in == ssa.Instruction(st) {
			return seen
		}
		if !seen {
			continue
		}
		switch in := in.(type) {
		case *ssa.Store, *ssa.Call, *ssa.Go, *ssa.Defer, *ssa.MapUpdate, *ssa.Send,
			*ssa.Select, *ssa.RunDefers, *ssa.Panic:
			return false
		case *ssa.UnOp:
			if in.Op == token.ARROW { // a receive synchronizes
				return false
			}
		}
	}
	return false
}
