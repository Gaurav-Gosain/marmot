package codegen

import (
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Package initialization code runs once, at startup, and is spread over
// the whole text of a program otherwise: each init function touches its
// own pages (tuios dev --version faulted on 306 text pages in init
// functions). With Options.InitSections the init functions of packages,
// and the functions nested in them (closures of variable initializers),
// go in a section the linker places together, as C++ static initializers:
// Mach-O __TEXT,__StaticInit; ELF .text.startup.SYM, which the default
// GNU ld and lld scripts gather next to each other.
func (m *moduleGen) initSection(fn *ssa.Function, sym string) string {
	if !m.p.opts.InitSections || !isInitCode(fn) {
		return ""
	}
	switch m.p.opts.GOOS {
	case "darwin", "ios":
		return "__TEXT,__StaticInit,regular,pure_instructions"
	case "linux", "android", "freebsd", "netbsd", "openbsd", "dragonfly", "illumos", "solaris":
		return ".text.startup." + sym
	}
	return ""
}

// isInitCode reports whether fn is a package init function (the
// synthesized init or a source func init, init#N) or nested in one.
func isInitCode(fn *ssa.Function) bool {
	for fn.Parent() != nil {
		fn = fn.Parent()
	}
	if fn.Pkg == nil || fn.Signature.Recv() != nil || fn.Signature.Params().Len() != 0 {
		return false
	}
	n := fn.Name()
	return n == "init" || strings.HasPrefix(n, "init#")
}
