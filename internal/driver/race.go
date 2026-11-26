package driver

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// -race builds the program with the race detector (design note:
// rtcore-design-race.md). Instrumentation: codegen marks the functions of
// the packages gc instruments with sanitize_thread, and the backend runs
// LLVM's ThreadSanitizer pass on them (-race). Runtime: build/lib/race holds
// librtcore.a and rtcore.bc built with RT_RACE=1 and the Go flavor of the TSan
// runtime (race_GOOS_GOARCH.o, from compiler-rt/lib/tsan/go, with the four
// names it shares with the C interface renamed to __gotsan_*). The executable
// is position dependent: TSan's Go mapping wants the program's data low and
// the heap at 0xc000000000. A build without -race is unchanged.

// checkRace rejects -race where the runtime cannot support it yet.
func (b *build) checkRace() error {
	if !b.cfg.Race {
		return nil
	}
	if b.goos != "linux" || b.goarch != "amd64" || b.cfg.Static || b.cross() {
		return fmt.Errorf("-race: only native linux/amd64 builds for now (not %s/%s%s)", b.goos, b.goarch, map[bool]string{true: ", -static"}[b.cfg.Static])
	}
	if obj := b.raceObject(); !fileExists(obj) {
		return fmt.Errorf("-race: %s is missing; run make -C runtime/core race", obj)
	}
	return nil
}

// raceObject is the TSan runtime object of the target.
func (b *build) raceObject() string {
	return b.rtLib(fmt.Sprintf("race_%s_%s.o", b.goos, b.goarch))
}

// raceLinkArgs follow the runtime library on the link line.
func (b *build) raceLinkArgs() []string {
	if !b.cfg.Race {
		return nil
	}
	return []string{b.raceObject()}
}

// loaderTags are the build tags: -tags, and race under -race (internal/race
// and the standard library's race annotations).
func (b *build) loaderTags() []string {
	if !b.cfg.Race || slices.Contains(b.cfg.Tags, "race") {
		return b.cfg.Tags
	}
	return append(slices.Clip(b.cfg.Tags), "race")
}

// dropRaceOff removes, in package runtime, every statement
// if raceenabled { ... } and if raceenabled && x { ... } (no init, no else)
// while raceenabled is the false constant of race_off.go: a build without
// -race then compiles the same code as before the race annotations existed.
// Without this, dev builds keep the dead blocks with their calls to the empty
// hooks, and the larger functions change inlining at -O2. Only the
// statements' lines remain different (the pc table). It runs before SSA, on
// the type-checked syntax; nothing else is changed.
func dropRaceOff(pkgs []*packages.Package) {
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.PkgPath != "runtime" || p.TypesInfo == nil {
			return
		}
		for _, f := range p.Syntax {
			astutil.Apply(f, func(c *astutil.Cursor) bool {
				if s, ok := c.Node().(*ast.IfStmt); ok && c.Index() >= 0 && s.Init == nil && s.Else == nil && raceOffCond(p.TypesInfo, s.Cond) {
					c.Delete()
					return false
				}
				return true
			}, nil)
		}
	})
}

// raceOffCond reports whether e is raceenabled, or a && chain whose first
// operand is raceenabled, with raceenabled the runtime's false constant.
func raceOffCond(info *types.Info, e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
			continue
		case *ast.BinaryExpr:
			if x.Op != token.LAND {
				return false
			}
			e = x.X
			continue
		case *ast.Ident:
			c, ok := info.Uses[x].(*types.Const)
			return ok && x.Name == "raceenabled" && c.Pkg() != nil && c.Pkg().Path() == "runtime" &&
				c.Val().Kind() == constant.Bool && !constant.BoolVal(c.Val())
		}
		return false
	}
}
