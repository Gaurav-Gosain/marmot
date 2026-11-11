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

// computeUsedMethods finds, by reachability from the program's roots, the
// interface method calls (name and signature) and the types whose values
// can be held in interfaces. Method code is kept for the pairs of both.
func (p *Program) computeUsedMethods() {
	p.globalRead = map[*ssa.Global]bool{}
	used := map[string]bool{}
	ifaceTypes := map[string]bool{}
	var ifaceList []types.Type
	seen := map[*ssa.Function]bool{}
	var queue []*ssa.Function
	push := func(fn *ssa.Function) {
		if fn != nil && !seen[fn] {
			seen[fn] = true
			queue = append(queue, fn)
			// Reflect method lookups, also when reached through the
			// reflect.Type interface, need every exported method.
			if p.isReflectMethodLookup(fn) {
				p.keepExported = true
			}
		}
	}
	// Roots: main, package initializers, the runtime (called by generated
	// code and the C core), and functions named by //go:linkname.
	remote := map[string]bool{}
	for _, v := range p.linkname {
		remote[v] = true
	}
	for _, pkg := range p.pkgs {
		path := pkg.Pkg.Path()
		rootPkg := path == "runtime" || strings.HasPrefix(path, "internal/runtime/")
		for _, fn := range p.byPkg[pkg] {
			_, pushed := p.linkname[fn]
			if rootPkg || pushed || remote[p.symbol(fn)] || fn.Name() == "main" && path == p.mainPath ||
				fn.Synthetic == "package initializer" || strings.HasPrefix(fn.Name(), "init#") {
				push(fn)
			}
		}
	}
	addIface := func(t types.Type) {
		for _, x := range p.typeClosure(t, ifaceTypes) {
			ifaceList = append(ifaceList, x)
		}
	}
	var ops []*ssa.Value
	scan := func(fn *ssa.Function) {
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
					if callee := cc.StaticCallee(); callee != nil {
						if p.isReflectMethodLookup(callee) {
							p.keepExported = true
						}
						if callee.Pkg != nil && callee.Pkg.Pkg.Path() == "reflect" &&
							(callee.Name() == "Call" || callee.Name() == "CallSlice") {
							p.reflectCall = true
						}
					}
				}
				if mi, ok := instr.(*ssa.MakeInterface); ok {
					addIface(mi.X.Type())
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
	// Fixpoint: reachable code adds used methods and interface types, whose
	// live methods are reachable code.
	done := 0
	for {
		for len(queue) > 0 {
			fn := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			scan(fn)
		}
		for ; done < len(ifaceList); done++ {
			t := ifaceList[done]
			if _, ok := t.Underlying().(*types.Interface); ok {
				continue
			}
			mset := p.prog.MethodSets.MethodSet(t)
			for i := 0; i < mset.Len(); i++ {
				if isGenericMethod(mset.At(i)) {
					continue
				}
				m := mset.At(i).Obj().(*types.Func)
				if used[p.methodKey(m)] || p.keepExported && m.Exported() {
					push(p.prog.MethodValue(mset.At(i)))
				}
			}
		}
		if len(queue) == 0 {
			// New used names may enable methods of types already processed.
			before := len(seen)
			for _, t := range ifaceList {
				if _, ok := t.Underlying().(*types.Interface); ok {
					continue
				}
				mset := p.prog.MethodSets.MethodSet(t)
				for i := 0; i < mset.Len(); i++ {
					if isGenericMethod(mset.At(i)) {
						continue
					}
					m := mset.At(i).Obj().(*types.Func)
					if used[p.methodKey(m)] || p.keepExported && m.Exported() {
						push(p.prog.MethodValue(mset.At(i)))
					}
				}
			}
			if len(seen) == before {
				break
			}
		}
	}
	p.usedMethods = used
	p.ifaceTypes = ifaceTypes
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
	if p.reflectCall {
		h.Write([]byte("reflectCall"))
	}
	tkeys := make([]string, 0, len(ifaceTypes))
	for k := range ifaceTypes {
		tkeys = append(tkeys, k)
	}
	sort.Strings(tkeys)
	for _, k := range tkeys {
		h.Write([]byte(k))
		h.Write([]byte{1})
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

// typeClosure adds t and every type reflect can reach from it (elements,
// fields, pointers, signatures) to set, returning the newly added types.
// Like gc's UsedInIface marking, only these types need method code.
func (p *Program) typeClosure(root types.Type, set map[string]bool) []types.Type {
	var added, stack []types.Type
	add := func(t types.Type) {
		t = types.Unalias(t)
		k := p.canonical(t)
		if !set[k] {
			set[k] = true
			stack = append(stack, t)
			added = append(added, t)
		}
	}
	add(root)
	for len(stack) > 0 {
		t := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := t.(*types.Named); ok {
			if _, isPtr := t.Underlying().(*types.Pointer); !isPtr {
				add(types.NewPointer(t)) // ptrToThis: reflect.New, Addr
			}
		}
		switch u := t.Underlying().(type) {
		case *types.Pointer:
			add(u.Elem())
		case *types.Slice:
			add(u.Elem())
		case *types.Array:
			add(u.Elem())
		case *types.Chan:
			add(u.Elem())
		case *types.Map:
			add(u.Key())
			add(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				add(u.Field(i).Type())
			}
		case *types.Signature:
			for i := 0; i < u.Params().Len(); i++ {
				add(u.Params().At(i).Type())
			}
			for i := 0; i < u.Results().Len(); i++ {
				add(u.Results().At(i).Type())
			}
		}
		if p.keepExported {
			mset := p.prog.MethodSets.MethodSet(t)
			for i := 0; i < mset.Len(); i++ {
				if !isGenericMethod(mset.At(i)) {
					add(stripRecv(mset.At(i).Obj().Type().(*types.Signature)))
				}
			}
		}
	}
	return added
}

// methodLive reports whether the code of method fn of type t must be kept.
func (p *Program) methodLive(fn *types.Func, t types.Type) bool {
	if p.usedMethods == nil {
		return true
	}
	if t != nil && !p.ifaceTypes[p.canonical(types.Unalias(t))] {
		return false // values of t never reach an interface
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
