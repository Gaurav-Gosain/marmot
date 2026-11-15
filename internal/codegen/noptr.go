package codegen

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Package variables whose type holds no pointer go in their own sections,
// which the collector does not scan (gc's noptrdata and noptrbss): the data
// and bss it scans conservatively shrink to the variables that can hold
// pointers. crypto/internal/fips140/drbg.memory alone is a 32 MB [N]byte
// that each stop of the world scanned word by word.
//
//	Mach-O: __DATA,__noptrdata and __DATA,__noptrbss (S_ZEROFILL)
//	ELF:    .data.marmot.noptr.SYM and .bss.marmot.noptr.SYM, one section
//	        per variable so --gc-sections still drops unused ones; the
//	        driver's linker script gathers them into .marmot_noptrdata and
//	        .marmot_noptrbss, between marmot_noptrdata_start and _end and
//	        marmot_noptrbss_start and _end
//
// The runtime (runtime/core) skips these ranges when it scans the image's
// writable segments.

// noptrSection returns the section for global g, or "" to leave it where
// LLVM puts it. zero: the initializer is all zeros.
func (m *moduleGen) noptrSection(g *ssa.Global, sym string, zero bool) string {
	if !m.p.opts.NoptrSections || hasPointers(g.Type().(*types.Pointer).Elem()) {
		return ""
	}
	// The runtime's own Go code may keep a heap address in a uintptr
	// variable; those stay scanned.
	if p := g.Pkg.Pkg.Path(); p == "runtime" || strings.HasPrefix(p, "runtime/") || strings.HasPrefix(p, "internal/runtime/") {
		return ""
	}
	return noptrSectionName(m.p.opts.GOOS, sym, zero)
}

// noptrSectionName is the pointer-free section of variable sym on goos
// ("": none, as on wasm).
func noptrSectionName(goos, sym string, zero bool) string {
	switch goos {
	case "darwin", "ios":
		if zero {
			return "__DATA,__noptrbss,zerofill"
		}
		return "__DATA,__noptrdata"
	case "linux", "android", "freebsd", "netbsd", "openbsd", "dragonfly", "illumos", "solaris":
		if zero {
			return ".bss.marmot.noptr." + sym
		}
		return ".data.marmot.noptr." + sym
	}
	return ""
}

// hasPointers reports whether a value of type t has a word the collector
// must scan: a pointer, unsafe.Pointer, string, slice, map, channel,
// function or interface. uintptr is not one (as in gc).
func hasPointers(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() == types.String || u.Kind() == types.UnsafePointer
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature, *types.Slice, *types.Interface:
		return true
	case *types.Array:
		return u.Len() > 0 && hasPointers(u.Elem())
	case *types.Struct:
		for f := range u.Fields() {
			if hasPointers(f.Type()) {
				return true
			}
		}
		return false
	}
	// Type parameters do not reach codegen; anything else is scanned.
	_, isTuple := t.(*types.Tuple)
	return !isTuple
}
