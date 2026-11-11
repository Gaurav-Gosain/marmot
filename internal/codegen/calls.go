package codegen

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// call emits a call and returns its result ("" if none).
func (f *funcGen) call(c *ssa.CallCommon, instr ssa.Instruction) string {
	if c.IsInvoke() {
		return f.invoke(c)
	}
	switch fn := c.Value.(type) {
	case *ssa.Builtin:
		return f.builtin(fn, c.Args, instr)
	case *ssa.Function:
		if len(fn.FreeVars) == 0 {
			args := make([]string, len(c.Args))
			for i, a := range c.Args {
				args[i] = f.value(a)
			}
			f.curArgs = c.Args
			if r, ok := f.intrinsic(fn, args); ok {
				return r
			}
			return f.directCall(fn, args)
		}
	}
	// Call through a func value: code pointer in word 0, closure as the
	// trailing hidden argument.
	fv := f.value(c.Value)
	f.nilCheck(fv)
	code := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", fv)
	sig := c.Signature()
	params, ret := f.m.sigTypes(sig, true)
	args := make([]string, 0, len(c.Args)+1)
	for _, a := range c.Args {
		args = append(args, f.value(a))
	}
	args = append(args, fv)
	return f.indirectCall(code, args, params, ret)
}

func (f *funcGen) indirectCall(code string, args, params []string, ret string) string {
	t := fmt.Sprintf("!llvm.ptr, (%s) -> %s", strings.Join(params, ", "), retOrVoid(ret))
	attr := ""
	if f.noinlineCalls {
		attr = " {no_inline}"
	}
	if ret == "" {
		f.emit("llvm.call %s(%s)%s : %s", code, strings.Join(args, ", "), attr, t)
		return ""
	}
	return f.op("llvm.call %s(%s)%s : %s", code, strings.Join(args, ", "), attr, t)
}

// invoke calls an interface method through the itab.
func (f *funcGen) invoke(c *ssa.CallCommon) string {
	if f.goOps() {
		x := f.value(c.Value)
		it := c.Value.Type().Underlying().(*types.Interface)
		idx := ifaceMethodIndex(it, c.Method)
		params, ret := f.m.sigTypes(stripRecv(c.Method.Type().(*types.Signature)), false)
		args := make([]string, len(c.Args))
		for i, a := range c.Args {
			args[i] = f.value(a)
		}
		text := fmt.Sprintf("go.iface_call %s[%d](%s) : (%s) -> %s", x, idx, strings.Join(args, ", "), strings.Join(params, ", "), retOrVoid(ret))
		if ret == "" {
			f.emit("%s", text)
			return ""
		}
		return f.op("%s", text)
	}
	x := f.value(c.Value)
	tab := f.extractRaw(x, tIface, 0)
	data := f.extractRaw(x, tIface, 1)
	f.nilCheck(tab)
	it := c.Value.Type().Underlying().(*types.Interface)
	idx := ifaceMethodIndex(it, c.Method)
	slot := f.gepByte(tab, int64(24+8*idx))
	code := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", slot)
	sig := c.Method.Type().(*types.Signature)
	params, ret := f.m.sigTypes(stripRecv(sig), false)
	params = append([]string{mlir.Ptr}, params...)
	args := []string{data}
	for _, a := range c.Args {
		args = append(args, f.value(a))
	}
	return f.indirectCall(code, args, params, ret)
}

// funcValueSym returns the symbol of a constant closure object for fn.
func (m *moduleGen) funcValueSym(fn *ssa.Function) string {
	target := m.funcRef(fn)
	if len(fn.FreeVars) > 0 {
		panic(fmt.Sprintf("funcValueSym: %v has free variables", fn))
	}
	wsym := target + "$fv"
	csym := target + "$fv.c"
	if m.defined[csym] {
		return csym
	}
	// Wrapper with the hidden closure parameter.
	params, ret := m.signature(fn)
	var b strings.Builder
	fmt.Fprintf(&b, "  llvm.func linkonce_odr %s(", mlir.Symbol(wsym))
	var args []string
	for i, t := range params {
		fmt.Fprintf(&b, "%%a%d: %s, ", i, t)
		args = append(args, fmt.Sprintf("%%a%d", i))
	}
	b.WriteString("%ctx: !llvm.ptr)")
	if ret != "" {
		b.WriteString(" -> " + ret)
	}
	b.WriteString(" attributes {no_unwind, passthrough = [[\"frame-pointer\", \"non-leaf\"]]} {\n")
	sig := fmt.Sprintf("(%s) -> %s", strings.Join(params, ", "), retOrVoid(ret))
	if ret == "" {
		fmt.Fprintf(&b, "    llvm.call %s(%s) : %s\n    llvm.return\n  }\n", mlir.Symbol(target), strings.Join(args, ", "), sig)
	} else {
		fmt.Fprintf(&b, "    %%r = llvm.call %s(%s) : %s\n    llvm.return %%r : %s\n  }\n", mlir.Symbol(target), strings.Join(args, ", "), sig, ret)
	}
	m.defined[wsym] = true
	m.defs.WriteString(b.String())
	m.constGlobal(csym, "linkonce_odr", true, cStruct(cAddr(wsym)))
	return csym
}

// makeClosure allocates a closure object {code, free variables...}.
func (f *funcGen) makeClosure(v *ssa.MakeClosure) {
	fn := v.Fn.(*ssa.Function)
	sym := f.m.funcRef(fn)
	fields := []*types.Var{types.NewField(token.NoPos, nil, "F", types.Typ[types.UnsafePointer], false)}
	for i, b := range v.Bindings {
		fields = append(fields, types.NewField(token.NoPos, nil, fmt.Sprintf("X%d", i), b.Type(), false))
	}
	st := types.NewStruct(fields, nil)
	if f.goOps() && !f.anyBig(v.Bindings) {
		args := make([]string, len(v.Bindings))
		tys := make([]string, len(v.Bindings))
		for i, b := range v.Bindings {
			args[i] = f.value(b)
			tys[i] = f.tm.valType(b.Type())
		}
		f.vals[v] = f.op("go.make_closure %s(%s), %s : (%s) -> !llvm.ptr", mlir.Symbol(sym),
			strings.Join(args, ", "), mlir.Symbol(f.m.typeDescSym(st)), strings.Join(tys, ", "))
		return
	}
	obj := f.newObject(st)
	mt := f.tm.memType(st)
	code := f.addressOf(sym)
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", code, obj)
	for i, b := range v.Bindings {
		p := f.gep(obj, mt, 0, i+1)
		f.store(f.value(b), p, b.Type())
	}
	f.vals[v] = obj
}

// builtin emits a call to a builtin function.
func (f *funcGen) builtin(b *ssa.Builtin, args []ssa.Value, instr ssa.Instruction) string {
	switch b.Name() {
	case "len", "cap":
		return f.lenCap(b.Name(), args[0])
	case "print", "println":
		f.print(b.Name() == "println", args)
		return ""
	case "append":
		return f.append(args[0], args[1])
	case "copy":
		dst := f.value(args[0])
		src := f.value(args[1])
		st := f.tm.valType(args[1].Type())
		dp, dl := f.extractRaw(dst, tSlice, 0), f.extractRaw(dst, tSlice, 1)
		sp, sl := f.extractRaw(src, st, 0), f.extractRaw(src, st, 1)
		es := f.constInt(f.elemSize(args[0].Type()), mlir.I64)
		return f.rtcall("slicecopy", dp, dl, sp, sl, es)
	case "ssa:wrapnilchk":
		p := f.value(args[0])
		f.nilCheck(p)
		return p
	case "real", "imag":
		x := f.value(args[0])
		i := 0
		if b.Name() == "imag" {
			i = 1
		}
		return f.extractRaw(x, f.tm.valType(args[0].Type()), i)
	case "complex":
		re, im := f.value(args[0]), f.value(args[1])
		return f.build(f.tm.valType(instr.(ssa.Value).Type()), re, im)
	case "min", "max":
		return f.minMax(b.Name() == "min", args, instr.(ssa.Value).Type())
	case "recover":
		fp := f.op("llvm.call_intrinsic \"llvm.frameaddress.p0\"(%s) : (i32) -> !llvm.ptr", f.constInt(0, mlir.I32))
		return f.rtcall("gorecover", f.op("llvm.ptrtoint %s : !llvm.ptr to i64", fp))
	case "ssa:deferstack":
		f.setjmp()
		return f.frame
	case "close":
		f.rtcall("closechan", f.value(args[0]))
		return ""
	case "delete":
		f.mapDelete(args[0], args[1])
		return ""
	case "clear":
		f.clear(args[0])
		return ""
	case "Sizeof", "Alignof":
		// Constant except in instantiated generic code.
		t := args[0].Type()
		n := f.m.p.opts.Sizes.Sizeof(t)
		if b.Name() == "Alignof" {
			n = f.m.p.opts.Sizes.Alignof(t)
		}
		return f.constInt(n, mlir.I64)
	case "Add": // unsafe.Add
		p := f.value(args[0])
		n := f.toInt64(f.value(args[1]), args[1].Type())
		return f.op("llvm.getelementptr %s[%s] : (!llvm.ptr, i64) -> !llvm.ptr, i8", p, n)
	case "StringData", "SliceData":
		x := f.value(args[0])
		return f.extractRaw(x, f.tm.valType(args[0].Type()), 0)
	case "String": // unsafe.String
		p := f.value(args[0])
		n := f.toInt64(f.value(args[1]), args[1].Type())
		return f.makeString(p, n)
	case "Slice": // unsafe.Slice
		p := f.value(args[0])
		n := f.toInt64(f.value(args[1]), args[1].Type())
		return f.build(tSlice, p, n, n)
	}
	f.fail(token.NoPos, "builtin %s is not supported yet", b.Name())
	return ""
}

// toInt64 widens an integer register value to i64.
func (f *funcGen) toInt64(x string, t types.Type) string {
	vt := f.tm.valType(t)
	if vt == mlir.I64 {
		return x
	}
	if isSigned(t) {
		return f.op("llvm.sext %s : %s to i64", x, vt)
	}
	return f.op("llvm.zext %s : %s to i64", x, vt)
}

func (f *funcGen) elemSize(sliceType types.Type) int64 {
	var et types.Type
	switch u := sliceType.Underlying().(type) {
	case *types.Slice:
		et = u.Elem()
	case *types.Basic:
		return 1
	case *types.Pointer:
		et = u.Elem().Underlying().(*types.Array).Elem()
	case *types.Array:
		et = u.Elem()
	}
	return f.m.p.opts.Sizes.Sizeof(et)
}

func (f *funcGen) lenCap(name string, x ssa.Value) string {
	t := x.Type()
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return f.extractRaw(f.value(x), tString, 1)
	case *types.Slice:
		i := 1
		if name == "cap" {
			i = 2
		}
		return f.extractRaw(f.value(x), tSlice, i)
	case *types.Array:
		return f.constInt(u.Len(), mlir.I64)
	case *types.Pointer:
		return f.constInt(u.Elem().Underlying().(*types.Array).Len(), mlir.I64)
	case *types.Map:
		return f.rtcall("maplen", f.value(x))
	case *types.Chan:
		if name == "cap" {
			return f.rtcall("chancap", f.value(x))
		}
		return f.rtcall("chanlen", f.value(x))
	}
	f.fail(token.NoPos, "%s of %v", name, t)
	return ""
}

func (f *funcGen) print(ln bool, args []ssa.Value) {
	f.rtcall("printlock")
	for i, a := range args {
		if ln && i > 0 {
			f.rtcall("printsp")
		}
		x := f.value(a)
		t := a.Type()
		switch u := t.Underlying().(type) {
		case *types.Basic:
			switch {
			case u.Info()&types.IsBoolean != 0:
				f.rtcall("printbool", x)
			case u.Info()&types.IsInteger != 0:
				v := f.toInt64(x, t)
				if u.Info()&types.IsUnsigned != 0 {
					f.rtcall("printuint", v)
				} else {
					f.rtcall("printint", v)
				}
			case u.Info()&types.IsFloat != 0:
				if u.Kind() == types.Float32 {
					f.rtcall("printfloat32", x)
				} else {
					f.rtcall("printfloat64", x)
				}
			case u.Info()&types.IsComplex != 0:
				if u.Kind() == types.Complex64 {
					f.rtcall("printcomplex64", x)
				} else {
					f.rtcall("printcomplex128", x)
				}
			case u.Info()&types.IsString != 0:
				f.rtcall("printstring", x)
			case u.Kind() == types.UnsafePointer:
				f.rtcall("printpointer", x)
			default:
				f.fail(a.Pos(), "cannot print %v", t)
			}
		case *types.Pointer, *types.Map, *types.Chan, *types.Signature:
			f.rtcall("printpointer", x)
		case *types.Slice:
			f.rtcall("printslice", f.extractRaw(x, tSlice, 0), f.extractRaw(x, tSlice, 1), f.extractRaw(x, tSlice, 2))
		case *types.Interface:
			if u.NumMethods() == 0 {
				f.rtcall("printeface", x)
			} else {
				f.rtcall("printiface", x)
			}
		default:
			f.fail(a.Pos(), "cannot print %v", t)
		}
	}
	if ln {
		f.rtcall("printnl")
	}
	f.rtcall("printunlock")
}

func (f *funcGen) minMax(isMin bool, args []ssa.Value, t types.Type) string {
	vt := f.tm.valType(t)
	acc := f.value(args[0])
	for _, a := range args[1:] {
		y := f.value(a)
		switch {
		case isInteger(t):
			pred := "slt"
			if !isSigned(t) {
				pred = "ult"
			}
			if !isMin {
				pred = strings.Replace(pred, "lt", "gt", 1)
			}
			c := f.op("llvm.icmp %q %s, %s : %s", pred, acc, y, vt)
			acc = f.op("llvm.select %s, %s, %s : i1, %s", c, acc, y, vt)
		case isFloat(t):
			name := "llvm.intr.maximum"
			if isMin {
				name = "llvm.intr.minimum"
			}
			acc = f.op("%s(%s, %s) : (%s, %s) -> %s", name, acc, y, vt, vt, vt)
		case isString(t):
			c := f.rtcall("cmpstring", acc, y)
			z := f.constInt(0, mlir.I64)
			pred := "slt"
			if !isMin {
				pred = "sgt"
			}
			lt := f.op("llvm.icmp %q %s, %s : i64", pred, c, z)
			acc = f.op("llvm.select %s, %s, %s : i1, %s", lt, acc, y, vt)
		default:
			f.fail(token.NoPos, "min/max of %v", t)
		}
	}
	return acc
}

// anyBig reports whether any value has a big (indirect) type; go dialect
// ops take values directly, so those keep the llvm form.
func (f *funcGen) anyBig(vs []ssa.Value) bool {
	for _, v := range vs {
		if f.tm.isBig(v.Type()) {
			return true
		}
	}
	return false
}
