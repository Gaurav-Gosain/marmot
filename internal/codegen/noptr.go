package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/types"
	"os"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
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
	if runtimePkg(g.Pkg.Pkg.Path()) {
		return ""
	}
	return m.noptrSectionOf(g.Type().(*types.Pointer).Elem(), sym, zero)
}

// noptrSectionOf is noptrSection for a variable of type t defined in this
// module (static literal objects too).
func (m *moduleGen) noptrSectionOf(t types.Type, sym string, zero bool) string {
	if !m.p.opts.NoptrSections || hasPointers(t) || m.pkg != nil && runtimePkg(m.pkg.Pkg.Path()) {
		return ""
	}
	return noptrSectionName(m.p.opts.GOOS, sym, zero)
}

// runtimePkg: the runtime's own Go code may keep a heap address in a
// uintptr variable, so its variables stay scanned conservatively.
func runtimePkg(p string) bool {
	return p == "runtime" || strings.HasPrefix(p, "runtime/") || strings.HasPrefix(p, "internal/runtime/")
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

// Package variables with pointers get a scan record each when
// Options.GCVars is set: {addr, nwords, bits}, bit i of bits set when word i
// of the variable may hold a pointer (the layout heap type bitmaps use). The
// runtime scans a variable with a record by its bits and conservatively
// only what no record and no pointer-free section covers (C data, runtime
// package variables). Records go in a section the linker concatenates, like
// the type links: Mach-O __DATA,__marmot_gcvars (a record keeps its
// variable alive), ELF marmot_gcvars (the backend
// links each record's section to its variable's, SHF_LINK_ORDER), found by
// section bounds and __start_/__stop_marmot_gcvars.
type gcvar struct {
	sym   string
	words []int64 // pointer word offsets
	n     int64   // words in the variable
}

// noteGCVar records variable g (symbol sym) for emitGCVars if it gets one.
func (m *moduleGen) noteGCVar(g *ssa.Global, sym string) {
	if !runtimePkg(g.Pkg.Pkg.Path()) {
		m.noteGCVarOf(g.Type().(*types.Pointer).Elem(), sym)
	}
}

// gcVarMinWords: smaller variables get no record. A record is 24 bytes and
// a conservative scan of a few words costs about what a precise one does.
const gcVarMinWords = 8

// noteGCVarOf is noteGCVar for a variable of type t defined in this module.
func (m *moduleGen) noteGCVarOf(t types.Type, sym string) {
	if !m.p.opts.GCVars || noptrSectionName(m.p.opts.GOOS, sym, false) == "" ||
		m.pkg != nil && runtimePkg(m.pkg.Pkg.Path()) {
		return
	}
	sizes := m.p.opts.Sizes
	ps := ptrSize(sizes)
	n := (sizes.Sizeof(t) + ps - 1) / ps
	if n < gcVarMinWords || n > maxBitmapWords || !hasPointers(t) {
		return // small, or too large for a bitmap: scanned conservatively
	}
	words := ptrWords(t, sizes)
	if int64(len(words)) == n {
		return // every word may be a pointer: the bitmap saves nothing
	}
	m.gcvars = append(m.gcvars, gcvar{sym: sym, words: words, n: n})
}

// emitGCVars writes the scan records of the variables noteGCVar collected.
func (m *moduleGen) emitGCVars() {
	if len(m.gcvars) == 0 {
		return
	}
	sec := "marmot_gcvars"
	if m.p.opts.GOOS == "darwin" || m.p.opts.GOOS == "ios" {
		// No live_support by default. The macOS linker's dead stripping of
		// live_support records does not always keep the same variables (one
		// private literal kept in some links and not in others), so two
		// links of the same objects could differ. Without it a record keeps
		// its variable alive. MARMOT_GCVARS_LIVE=1 restores live_support.
		sec = "__DATA,__marmot_gcvars,regular"
		if os.Getenv("MARMOT_GCVARS_LIVE") == "1" {
			sec += ",live_support"
		}
	}
	rec := mlir.Struct(mlir.Ptr, mlir.I64, mlir.Ptr)
	var b strings.Builder
	for i, v := range m.gcvars {
		bm := make([]byte, (v.n+7)/8)
		for _, w := range v.words {
			bm[w/8] |= 1 << (w % 8)
		}
		h := sha256.Sum256(bm)
		bits := "go:gcbits." + hex.EncodeToString(h[:12])
		if !m.defined[bits] {
			elems := make([]cv, len(bm))
			for j, x := range bm {
				elems[j] = cInt(int64(x), mlir.I8)
			}
			m.constGlobal(bits, "linkonce_odr", true, cArray(mlir.I8, elems...))
		}
		name := fmt.Sprintf("marmot.gcvar.%d", i)
		fmt.Fprintf(&b, "  llvm.mlir.global internal constant @%s() {addr_space = 0 : i32, alignment = 8 : i64, section = %q} : %s {\n", name, sec, rec)
		n := 0
		val := writeCV(&b, &n, cAgg(rec, cAddr(v.sym), cI64(v.n), cAddr(bits)))
		fmt.Fprintf(&b, "    llvm.return %s : %s\n  }\n", val, rec)
		m.compilerUsed = append(m.compilerUsed, cAddr(name))
	}
	m.defs.WriteString(b.String())
}
