package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Method pruning, like the gc linker's dead code elimination: a method's
// code is referenced from method tables and itabs only if some interface
// method call in the program uses its name and signature. Other entries
// are nil, so the method can be dead stripped. The analysis over-approximates
// (it scans every function the program could compile), which is safe.

// methodKey identifies a method by name and signature, as interface method
// calls and itab construction match them.
func (p *Program) methodKey(fn *types.Func) string {
	return methodName(fn) + p.canonical(stripRecv(fn.Type().(*types.Signature)))
}

// computeUsedMethods scans the program for interface method calls.
func (p *Program) computeUsedMethods() {
	p.globalRead = map[*ssa.Global]bool{}
	used := map[string]bool{}
	seen := map[*ssa.Function]bool{}
	var queue []*ssa.Function
	push := func(fn *ssa.Function) {
		if fn != nil && !seen[fn] {
			seen[fn] = true
			queue = append(queue, fn)
		}
	}
	for _, pkg := range p.pkgs {
		for _, fn := range p.byPkg[pkg] {
			push(fn)
		}
	}
	addMethods := func(t types.Type) {
		for _, t := range []types.Type{t, types.NewPointer(t)} {
			if _, ok := t.Underlying().(*types.Interface); ok {
				continue
			}
			mset := p.prog.MethodSets.MethodSet(t)
			for i := 0; i < mset.Len(); i++ {
				push(p.prog.MethodValue(mset.At(i)))
			}
		}
	}
	var ops []*ssa.Value
	for len(queue) > 0 {
		fn := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, a := range fn.AnonFuncs {
			push(a)
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				if c, ok := instr.(ssa.CallInstruction); ok {
					cc := c.Common()
					if cc.IsInvoke() {
						used[p.methodKey(cc.Method)] = true
					}
					if callee := cc.StaticCallee(); callee != nil && p.isReflectMethodLookup(callee) {
						p.keepExported = true
					}
				}
				if mi, ok := instr.(*ssa.MakeInterface); ok {
					addMethods(mi.X.Type())
				}
				ops = instr.Operands(ops[:0])
				for _, o := range ops {
					if o == nil {
						continue
					}
					switch x := (*o).(type) {
					case *ssa.Function:
						push(x)
					case *ssa.Global:
						if !isInitWrite(fn, instr, x) {
							p.globalRead[x] = true
						}
					}
				}
			}
		}
	}
	p.usedMethods = used
	keys := make([]string, 0, len(used))
	for k := range used {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
	}
	if p.keepExported {
		h.Write([]byte("keepExported"))
	}
	p.usedMethodsHash = hex.EncodeToString(h.Sum(nil))[:16]
}

// isReflectMethodLookup reports whether fn looks up methods by index or
// name through reflect, which needs every exported method.
func (p *Program) isReflectMethodLookup(fn *ssa.Function) bool {
	if fn.Pkg == nil || fn.Pkg.Pkg.Path() != "reflect" {
		return false
	}
	switch fn.Name() {
	case "Method", "MethodByName":
		return true
	}
	return false
}

// methodLive reports whether the code of method fn of a type must be kept.
func (p *Program) methodLive(fn *types.Func) bool {
	if p.usedMethods == nil {
		return true
	}
	if p.keepExported && fn.Exported() {
		return true
	}
	return p.usedMethods[p.methodKey(fn)]
}

// UsedMethodsHash identifies the pruning result; every module depends on it.
func (p *Program) UsedMethodsHash() string { return p.usedMethodsHash }

// isInitWrite reports whether the use of global g by instr in fn only
// writes g: a store in g's package initializer, directly or through field
// and element addresses that are only stored to.
func isInitWrite(fn *ssa.Function, instr ssa.Instruction, g *ssa.Global) bool {
	if fn.Synthetic != "package initializer" || fn.Pkg != g.Pkg {
		return false
	}
	var onlyStores func(v ssa.Value) bool
	onlyStores = func(v ssa.Value) bool {
		for _, r := range *v.Referrers() {
			switch x := r.(type) {
			case *ssa.Store:
				if x.Addr != v {
					return false
				}
			case *ssa.FieldAddr:
				if !onlyStores(x) {
					return false
				}
			case *ssa.IndexAddr:
				if !onlyStores(x) {
					return false
				}
			case *ssa.DebugRef:
			default:
				return false
			}
		}
		return true
	}
	switch x := instr.(type) {
	case *ssa.Store:
		return x.Addr == ssa.Value(g)
	case *ssa.FieldAddr:
		return onlyStores(x)
	case *ssa.IndexAddr:
		return onlyStores(x)
	}
	return false
}

// globalDead reports whether package variable g is never read by the
// program, so its initialization can be dropped when it has no side effects.
func (p *Program) globalDead(g *ssa.Global) bool {
	if p.globalRead == nil || p.globalRead[g] || strings.Contains(g.Name(), "$") {
		return false
	}
	if p.varLinkname[g.Pkg.Pkg.Path()+"."+g.Name()] {
		return false // referenced by symbol name elsewhere
	}
	return true
}
