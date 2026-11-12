package codegen

import (
	"os"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Lifetime transforms: every transform that frees or reuses memory or
// changes object lifetime has its own switch and is OFF by default in dev
// and -release until its rule is proved or checked, its checker runs clean
// over tests/run and tests/world, and adversarial tests pass under
// poison-on-free. Switches are environment
// variables set to 1; MARMOT_LIFETIME=all turns every one on.
//
//	MARMOT_REUSE         in-place reuse (rt_free of the old array on append growth)
//	MARMOT_REGIONS       region inference (region clones)
//	MARMOT_FRAME_REGIONS frame regions (region-parametric callees)
//	MARMOT_ALLOCSINK     allocation sinking
//	MARMOT_ESCAPE_SUMMARY heap to stack through escape summaries of callees and
//	                     closures (summary based stack allocation)
//	MARMOT_XPKG_ESCAPE   also use summaries of callees in other packages
//	MARMOT_ESCAPE_EXT    escape rules added 2026-11-12 (results, variadic, byte
//	                     reads, string ranges, map keys, unsafe builtins, append,
//	                     //go:noescape body-less functions)
//	MARMOT_STACKCONV     frame buffers for string(b), []byte(s) and small make
//	MARMOT_BYVALUE       closures capture settled variables by value
//
// Other transforms where a wrong analysis changes behavior, same
// switches and default:
//
//	MARMOT_MAPFUSE       v := m[k]; ...; m[k] = w as one mapassign
//	MARMOT_APPENDMAKE    append(s, make([]T, n)...) without the temporary
//	MARMOT_DEFERELIDE    open-coded defers without the _defer record when
//	                     nothing may panic
//	MARMOT_DEADINIT      drop initializers of package variables nothing reads
//	                     (only computations that cannot panic; spec audit)
//
// What stays on: intraprocedural escape analysis (a value passed to any
// call, other than len, cap, copy and print builtins, escapes).

var lifetimeAll = os.Getenv("MARMOT_LIFETIME") == "all"

func lifetimeOn(name string) bool {
	return lifetimeAll || os.Getenv(name) == "1"
}

var (
	optReuse        = lifetimeOn("MARMOT_REUSE")
	optRegions      = lifetimeOn("MARMOT_REGIONS")
	optFrameRegions = lifetimeOn("MARMOT_FRAME_REGIONS")
	optAllocSink    = lifetimeOn("MARMOT_ALLOCSINK")
	optEscSummary   = lifetimeOn("MARMOT_ESCAPE_SUMMARY")
	optXPkgEscape   = lifetimeOn("MARMOT_XPKG_ESCAPE")
	optEscapeExt    = lifetimeOn("MARMOT_ESCAPE_EXT")
	optStackConv    = lifetimeOn("MARMOT_STACKCONV")
	optByValue      = lifetimeOn("MARMOT_BYVALUE")
	optMapFuse      = lifetimeOn("MARMOT_MAPFUSE")
	optAppendMake   = lifetimeOn("MARMOT_APPENDMAKE")
	optDeferElide   = lifetimeOn("MARMOT_DEFERELIDE")
	optDeadInit     = lifetimeOn("MARMOT_DEADINIT")
)

// LifetimeEnv lists the switch variables (for cache keys).
var LifetimeEnv = []string{"MARMOT_LIFETIME", "MARMOT_REUSE", "MARMOT_REGIONS", "MARMOT_FRAME_REGIONS",
	"MARMOT_ALLOCSINK", "MARMOT_ESCAPE_SUMMARY", "MARMOT_XPKG_ESCAPE", "MARMOT_ESCAPE_EXT", "MARMOT_STACKCONV", "MARMOT_BYVALUE",
	"MARMOT_MAPFUSE", "MARMOT_APPENDMAKE", "MARMOT_DEFERELIDE", "MARMOT_DEADINIT"}

// LifetimeSummary describes which lifetime transforms are on.
func LifetimeSummary() string {
	var on []string
	for _, k := range LifetimeEnv[1:] {
		if lifetimeOn(k) {
			on = append(on, strings.TrimPrefix(k, "MARMOT_"))
		}
	}
	if len(on) == 0 {
		return "none"
	}
	return strings.Join(on, ",")
}

// parentPkg returns the package of the function that defines v.
func parentPkg(v ssa.Value) *ssa.Package {
	var fn *ssa.Function
	switch x := v.(type) {
	case ssa.Instruction:
		fn = x.Parent()
	case *ssa.Parameter:
		fn = x.Parent()
	case *ssa.FreeVar:
		fn = x.Parent()
	}
	if fn == nil {
		return nil
	}
	if fn.Pkg == nil && fn.Origin() != nil {
		return fn.Origin().Pkg
	}
	return fn.Pkg
}

// summaryAllowed reports whether a caller in the package of v may use the
// escape summary of callee.
func summaryAllowed(v ssa.Value, callee *ssa.Function) bool {
	if !optEscSummary {
		return false
	}
	if optXPkgEscape {
		return true
	}
	cp := callee.Pkg
	if cp == nil && callee.Origin() != nil {
		cp = callee.Origin().Pkg
	}
	vp := parentPkg(v)
	return cp != nil && vp != nil && cp == vp
}
