package codegen

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Type descriptors follow lib/src/runtime/type.go exactly.

const (
	kindBool = 1 + iota
	kindInt
	kindInt8
	kindInt16
	kindInt32
	kindInt64
	kindUint
	kindUint8
	kindUint16
	kindUint32
	kindUint64
	kindUintptr
	kindFloat32
	kindFloat64
	kindComplex64
	kindComplex128
	kindArray
	kindChan
	kindFunc
	kindInterface
	kindMap
	kindPointer
	kindSlice
	kindString
	kindStruct
	kindUnsafePointer
)

const (
	tflagNamed         = 1 << 0
	tflagComparable    = 1 << 1
	tflagRegularMemory = 1 << 2
	tflagDirectIface   = 1 << 3
)

// maxBitmapWords bounds the pointer bitmap size; larger types are scanned
// conservatively (gcdata nil).
const maxBitmapWords = 4096

var tHeader = mlir.Struct(mlir.I64, mlir.I64, mlir.Ptr, tString, mlir.Ptr, mlir.I8, mlir.I8, mlir.I8, mlir.I8)

// typeSym returns the canonical descriptor symbol for t.
func (p *Program) typeSym(t types.Type) string {
	return "type:" + p.canonical(t)
}

// canonical is a unique, stable name for t used in symbols: full package
// paths, aliases resolved, local named types qualified by position.
func (p *Program) canonical(t types.Type) string {
	var b strings.Builder
	p.writeCanonical(&b, t)
	return b.String()
}

func (p *Program) writeCanonical(b *strings.Builder, t types.Type) {
	switch u := types.Unalias(t).(type) {
	case *types.Basic:
		if u.Kind() == types.UnsafePointer {
			b.WriteString("unsafe.Pointer")
		} else {
			b.WriteString(u.Name())
		}
	case *types.Named:
		obj := u.Obj()
		if obj.Pkg() != nil {
			b.WriteString(obj.Pkg().Path())
			b.WriteByte('.')
		}
		b.WriteString(obj.Name())
		if obj.Pkg() != nil && obj.Parent() != nil && obj.Parent() != obj.Pkg().Scope() {
			pos := p.fset.Position(obj.Pos())
			fmt.Fprintf(b, "·%d.%d", pos.Line, pos.Column)
		}
		if ta := u.TypeArgs(); ta != nil && ta.Len() > 0 {
			b.WriteByte('[')
			for i := 0; i < ta.Len(); i++ {
				if i > 0 {
					b.WriteByte(',')
				}
				p.writeCanonical(b, ta.At(i))
			}
			b.WriteByte(']')
		}
	case *types.Pointer:
		b.WriteByte('*')
		p.writeCanonical(b, u.Elem())
	case *types.Slice:
		b.WriteString("[]")
		p.writeCanonical(b, u.Elem())
	case *types.Array:
		fmt.Fprintf(b, "[%d]", u.Len())
		p.writeCanonical(b, u.Elem())
	case *types.Map:
		b.WriteString("map[")
		p.writeCanonical(b, u.Key())
		b.WriteByte(']')
		p.writeCanonical(b, u.Elem())
	case *types.Chan:
		switch u.Dir() {
		case types.SendRecv:
			b.WriteString("chan(")
		case types.SendOnly:
			b.WriteString("chan<-(")
		case types.RecvOnly:
			b.WriteString("<-chan(")
		}
		p.writeCanonical(b, u.Elem())
		b.WriteByte(')')
	case *types.Signature:
		b.WriteString("func(")
		for i := 0; i < u.Params().Len(); i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			if u.Variadic() && i == u.Params().Len()-1 {
				b.WriteString("...")
			}
			p.writeCanonical(b, u.Params().At(i).Type())
		}
		b.WriteString(")(")
		for i := 0; i < u.Results().Len(); i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			p.writeCanonical(b, u.Results().At(i).Type())
		}
		b.WriteByte(')')
	case *types.Interface:
		b.WriteString("interface{")
		for i, m := range sortedIfaceMethods(u) {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteString(methodName(m))
			p.writeCanonical(b, m.Type())
		}
		b.WriteByte('}')
	case *types.Struct:
		b.WriteString("struct{")
		for i := 0; i < u.NumFields(); i++ {
			if i > 0 {
				b.WriteByte(';')
			}
			f := u.Field(i)
			if f.Embedded() {
				b.WriteString("~")
			}
			if !f.Exported() && f.Pkg() != nil {
				b.WriteString(f.Pkg().Path() + ".")
			}
			b.WriteString(f.Name())
			b.WriteByte(' ')
			p.writeCanonical(b, f.Type())
			if tag := u.Tag(i); tag != "" {
				b.WriteString(strconv.Quote(tag))
			}
		}
		b.WriteByte('}')
	case *types.Tuple:
		b.WriteByte('(')
		for i := 0; i < u.Len(); i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			p.writeCanonical(b, u.At(i).Type())
		}
		b.WriteByte(')')
	case *types.TypeParam:
		b.WriteString(u.Obj().Name())
	default:
		b.WriteString(t.String())
	}
}

// typeDesc returns a pointer to the descriptor of t.
func (f *funcGen) typeDesc(t types.Type) string {
	return f.addressOf(f.m.typeDescSym(t))
}

// typeDescSym makes the descriptor of t available and returns its symbol.
func (m *moduleGen) typeDescSym(t types.Type) string {
	t = types.Unalias(t)
	sym := m.p.typeSym(t)
	if m.defined[sym] {
		return sym
	}
	m.defined[sym] = true
	m.emitTypeDesc(sym, t)
	return sym
}

func (m *moduleGen) emitTypeDesc(sym string, t types.Type) {
	sizes := m.p.opts.Sizes
	size := sizes.Sizeof(t)
	align := sizes.Alignof(t)
	words := ptrWords(t, sizes)
	var ptrdata int64
	gcdata := cNull()
	if len(words) > 0 {
		last := words[len(words)-1]
		ptrdata = (last + 1) * 8
		if last < maxBitmapWords {
			bm := make([]byte, (last+8)/8)
			for _, w := range words {
				bm[w/8] |= 1 << (w % 8)
			}
			gsym := sym + ".gcdata"
			elems := make([]cv, len(bm))
			for i, x := range bm {
				elems[i] = cInt(int64(x), mlir.I8)
			}
			m.constGlobal(gsym, "linkonce_odr", true, cArray(mlir.I8, elems...))
			gcdata = cAddr(gsym)
		}
	}
	var tflag int64
	switch types.Unalias(t).(type) {
	case *types.Named, *types.Basic:
		tflag |= tflagNamed
	}
	if types.Comparable(t) {
		tflag |= tflagComparable
		if isRegularMemory(t, sizes) {
			tflag |= tflagRegularMemory
		}
	}
	if isDirectIface(t) {
		tflag |= tflagDirectIface
	}
	uncommon := cNull()
	if u := m.uncommon(sym, t); u != "" {
		uncommon = cAddr(u)
	}
	header := cAgg(tHeader,
		cI64(size), cI64(ptrdata), gcdata, m.cString(runtimeTypeString(t)), uncommon,
		cInt(kindOf(t), mlir.I8), cInt(align, mlir.I8), cInt(align, mlir.I8), cInt(tflag, mlir.I8))

	var desc cv
	switch u := t.Underlying().(type) {
	case *types.Pointer:
		desc = cStruct(header, cAddr(m.typeDescSym(u.Elem())))
	case *types.Slice:
		desc = cStruct(header, cAddr(m.typeDescSym(u.Elem())))
	case *types.Array:
		desc = cStruct(header, cAddr(m.typeDescSym(u.Elem())), cI64(u.Len()))
	case *types.Chan:
		dir := int64(3)
		switch u.Dir() {
		case types.RecvOnly:
			dir = 1
		case types.SendOnly:
			dir = 2
		}
		desc = cStruct(header, cAddr(m.typeDescSym(u.Elem())), cI64(dir))
	case *types.Map:
		desc = cStruct(header, cAddr(m.typeDescSym(u.Key())), cAddr(m.typeDescSym(u.Elem())))
	case *types.Signature:
		n := u.Params().Len() + u.Results().Len()
		params := cNull()
		if n > 0 {
			var es []cv
			for i := 0; i < u.Params().Len(); i++ {
				es = append(es, cAddr(m.typeDescSym(u.Params().At(i).Type())))
			}
			for i := 0; i < u.Results().Len(); i++ {
				es = append(es, cAddr(m.typeDescSym(u.Results().At(i).Type())))
			}
			psym := sym + ".params"
			m.constGlobal(psym, "linkonce_odr", true, cArray(mlir.Ptr, es...))
			params = cAddr(psym)
		}
		variadic := int64(0)
		if u.Variadic() {
			variadic = 1
		}
		desc = cStruct(header, params, cInt(int64(u.Params().Len()), mlir.I16),
			cInt(int64(u.Results().Len()), mlir.I16), cInt(variadic, mlir.I8))
	case *types.Interface:
		ms := sortedIfaceMethods(u)
		methods := cNull()
		if len(ms) > 0 {
			var es []cv
			for _, fn := range ms {
				es = append(es, cStruct(m.cString(methodName(fn)), cAddr(m.typeDescSym(stripRecv(fn.Type().(*types.Signature))))))
			}
			msym := sym + ".imethods"
			m.constGlobal(msym, "linkonce_odr", true, cArray(mlir.Struct(tString, mlir.Ptr), es...))
			methods = cAddr(msym)
		}
		desc = cStruct(header, m.cString(pkgPathOf(t)), methods, cI64(int64(len(ms))))
	case *types.Struct:
		fields := cNull()
		if u.NumFields() > 0 {
			offs := sizes.Offsetsof(structFields(u))
			var es []cv
			for i := 0; i < u.NumFields(); i++ {
				fld := u.Field(i)
				name := fld.Name()
				emb := int64(0)
				if fld.Embedded() {
					emb = 1
				}
				es = append(es, cStruct(m.cString(name), m.cString(u.Tag(i)),
					cAddr(m.typeDescSym(fld.Type())), cI64(offs[i]<<1|emb)))
			}
			fsym := sym + ".fields"
			m.constGlobal(fsym, "linkonce_odr", true, cArray(mlir.Struct(tString, tString, mlir.Ptr, mlir.I64), es...))
			fields = cAddr(fsym)
		}
		desc = cStruct(header, m.cString(pkgPathOf(t)), fields, cI64(int64(u.NumFields())))
	default:
		desc = header
	}
	m.constGlobal(sym, "linkonce_odr", true, desc)
}

func structFields(st *types.Struct) []*types.Var {
	vs := make([]*types.Var, st.NumFields())
	for i := range vs {
		vs[i] = st.Field(i)
	}
	return vs
}

func stripRecv(sig *types.Signature) *types.Signature {
	return types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
}

// pkgPathOf returns the defining package path for named types, "" otherwise.
func pkgPathOf(t types.Type) string {
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil {
		return n.Obj().Pkg().Path()
	}
	return ""
}

// methodName is the runtime method name: bare when exported, else
// qualified with the package path.
func methodName(fn *types.Func) string {
	if fn.Exported() || fn.Pkg() == nil {
		return fn.Name()
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// sortedIfaceMethods returns the methods of an interface in runtime order.
func sortedIfaceMethods(it *types.Interface) []*types.Func {
	ms := make([]*types.Func, it.NumMethods())
	for i := range ms {
		ms[i] = it.Method(i)
	}
	sort.Slice(ms, func(i, j int) bool { return methodName(ms[i]) < methodName(ms[j]) })
	return ms
}

// ifaceMethodIndex returns the itab slot of method fn in interface it.
func ifaceMethodIndex(it *types.Interface, fn *types.Func) int {
	name := methodName(fn)
	for i, m := range sortedIfaceMethods(it) {
		if methodName(m) == name {
			return i
		}
	}
	panic("method not in interface: " + name)
}

// uncommon emits the uncommontype of t if it needs one, returning its symbol.
func (m *moduleGen) uncommon(sym string, t types.Type) string {
	_, named := types.Unalias(t).(*types.Named)
	ms := m.p.methodSet(t)
	if !named && len(ms) == 0 {
		return ""
	}
	if _, ok := t.Underlying().(*types.Interface); ok {
		ms = nil
	}
	methods := cNull()
	var xcount int64
	if len(ms) > 0 {
		var es []cv
		for _, sel := range ms {
			fn := sel.Obj().(*types.Func)
			if fn.Exported() {
				xcount++
			}
			ifn := cNull()
			if s := m.ifaceFunc(t, sel); s != "" {
				ifn = cAddr(s)
			}
			es = append(es, cStruct(m.cString(methodName(fn)),
				cAddr(m.typeDescSym(stripRecv(fn.Type().(*types.Signature)))), ifn))
		}
		msym := sym + ".methods"
		m.constGlobal(msym, "linkonce_odr", true, cArray(mlir.Struct(tString, mlir.Ptr, mlir.Ptr), es...))
		methods = cAddr(msym)
	}
	usym := sym + ".uncommon"
	m.constGlobal(usym, "linkonce_odr", true, cStruct(m.cString(pkgPathOf(t)), methods,
		cInt(int64(len(ms)), mlir.I32), cInt(xcount, mlir.I32)))
	return usym
}

// methodSet returns the method selections of t in runtime order.
func (p *Program) methodSet(t types.Type) []*types.Selection {
	mset := p.prog.MethodSets.MethodSet(t)
	var out []*types.Selection
	for i := 0; i < mset.Len(); i++ {
		out = append(out, mset.At(i))
	}
	sort.Slice(out, func(i, j int) bool {
		return methodName(out[i].Obj().(*types.Func)) < methodName(out[j].Obj().(*types.Func))
	})
	return out
}

// ifaceFunc returns the symbol of the itab entry for method sel of t: a
// function taking the interface data word as its first parameter.
func (m *moduleGen) ifaceFunc(t types.Type, sel *types.Selection) string {
	fn := m.p.prog.MethodValue(sel)
	if fn == nil {
		return ""
	}
	if m.tm.valType(t) == mlir.Ptr {
		return m.funcRef(fn)
	}
	// The data word points at a boxed value (or, for direct single field
	// aggregates, is the value's only word): load it and call the method.
	target := m.funcRef(fn)
	wsym := target + "$ifn"
	if m.defined[wsym] {
		return wsym
	}
	m.defined[wsym] = true
	params, ret := m.signature(fn)
	var b strings.Builder
	fmt.Fprintf(&b, "  llvm.func linkonce_odr %s(%%a0: !llvm.ptr", mlir.Symbol(wsym))
	for i := 1; i < len(params); i++ {
		fmt.Fprintf(&b, ", %%a%d: %s", i, params[i])
	}
	b.WriteString(")")
	if ret != "" {
		b.WriteString(" -> " + ret)
	}
	b.WriteString(" attributes {no_unwind, passthrough = [[\"frame-pointer\", \"non-leaf\"]]} {\n")
	recvT := params[0]
	src := "%a0"
	if isDirectIface(t) {
		fmt.Fprintf(&b, "    %%one = llvm.mlir.constant(1 : i64) : i64\n")
		fmt.Fprintf(&b, "    %%slot = llvm.alloca %%one x !llvm.ptr : (i64) -> !llvm.ptr\n")
		fmt.Fprintf(&b, "    llvm.store %%a0, %%slot : !llvm.ptr, !llvm.ptr\n")
		src = "%slot"
	}
	if isBool(t) {
		fmt.Fprintf(&b, "    %%r8 = llvm.load %s : !llvm.ptr -> i8\n    %%recv = llvm.trunc %%r8 : i8 to i1\n", src)
	} else {
		fmt.Fprintf(&b, "    %%recv = llvm.load %s : !llvm.ptr -> %s\n", src, recvT)
	}
	args := []string{"%recv"}
	for i := 1; i < len(params); i++ {
		args = append(args, fmt.Sprintf("%%a%d", i))
	}
	sig := fmt.Sprintf("(%s) -> %s", strings.Join(params, ", "), retOrVoid(ret))
	if ret == "" {
		fmt.Fprintf(&b, "    llvm.call %s(%s) : %s\n    llvm.return\n  }\n", mlir.Symbol(target), strings.Join(args, ", "), sig)
	} else {
		fmt.Fprintf(&b, "    %%r = llvm.call %s(%s) : %s\n    llvm.return %%r : %s\n  }\n", mlir.Symbol(target), strings.Join(args, ", "), sig, ret)
	}
	m.defs.WriteString(b.String())
	return wsym
}

// itabSym makes the itab for concrete type t and interface type it.
func (m *moduleGen) itabSym(t, iface types.Type) string {
	sym := "itab:" + m.p.canonical(t) + "," + m.p.canonical(iface)
	if m.defined[sym] {
		return sym
	}
	m.defined[sym] = true
	it := iface.Underlying().(*types.Interface)
	ms := sortedIfaceMethods(it)
	funs := make([]cv, len(ms))
	mset := m.p.prog.MethodSets.MethodSet(t)
	for i, im := range ms {
		sel := mset.Lookup(im.Pkg(), im.Name())
		if sel == nil {
			panic(fmt.Sprintf("itab: %v does not implement %v (missing %s)", t, iface, im.Name()))
		}
		funs[i] = cAddr(m.ifaceFunc(t, sel))
	}
	m.constGlobal(sym, "linkonce_odr", true, cStruct(
		cAddr(m.typeDescSym(iface)), cAddr(m.typeDescSym(t)), cInt(0, mlir.I32), cInt(0, mlir.I32),
		cArray(mlir.Ptr, funs...)))
	return sym
}

func kindOf(t types.Type) int64 {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Bool:
			return kindBool
		case types.Int:
			return kindInt
		case types.Int8:
			return kindInt8
		case types.Int16:
			return kindInt16
		case types.Int32:
			return kindInt32
		case types.Int64:
			return kindInt64
		case types.Uint:
			return kindUint
		case types.Uint8:
			return kindUint8
		case types.Uint16:
			return kindUint16
		case types.Uint32:
			return kindUint32
		case types.Uint64:
			return kindUint64
		case types.Uintptr:
			return kindUintptr
		case types.Float32:
			return kindFloat32
		case types.Float64:
			return kindFloat64
		case types.Complex64:
			return kindComplex64
		case types.Complex128:
			return kindComplex128
		case types.String:
			return kindString
		case types.UnsafePointer:
			return kindUnsafePointer
		}
	case *types.Array:
		return kindArray
	case *types.Chan:
		return kindChan
	case *types.Signature:
		return kindFunc
	case *types.Interface:
		return kindInterface
	case *types.Map:
		return kindMap
	case *types.Pointer:
		return kindPointer
	case *types.Slice:
		return kindSlice
	case *types.Struct:
		return kindStruct
	}
	panic(fmt.Sprintf("kindOf %v", t))
}

// ptrWords lists the pointer-sized words of t that hold pointers.
func ptrWords(t types.Type, sizes types.Sizes) []int64 {
	var out []int64
	var walk func(t types.Type, off int64)
	walk = func(t types.Type, off int64) {
		switch u := t.Underlying().(type) {
		case *types.Basic:
			if u.Kind() == types.String || u.Kind() == types.UnsafePointer {
				out = append(out, off/8)
			}
		case *types.Pointer, *types.Map, *types.Chan, *types.Signature, *types.Slice:
			out = append(out, off/8)
		case *types.Interface:
			out = append(out, off/8, off/8+1)
		case *types.Array:
			es := sizes.Sizeof(u.Elem())
			if es == 0 || len(ptrWords(u.Elem(), sizes)) == 0 {
				return
			}
			for i := int64(0); i < u.Len(); i++ {
				walk(u.Elem(), off+i*es)
				if len(out) > maxBitmapWords*2 {
					return
				}
			}
		case *types.Struct:
			offs := sizes.Offsetsof(structFields(u))
			for i := 0; i < u.NumFields(); i++ {
				walk(u.Field(i).Type(), off+offs[i])
			}
		}
	}
	walk(t, 0)
	return out
}

// isRegularMemory: comparable with plain byte comparison (see type.go).
func isRegularMemory(t types.Type, sizes types.Sizes) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		i := u.Info()
		if i&(types.IsFloat|types.IsComplex|types.IsString) != 0 {
			return false
		}
		return true
	case *types.Pointer, *types.Chan:
		return true
	case *types.Array:
		return isRegularMemory(u.Elem(), sizes)
	case *types.Struct:
		offs := sizes.Offsetsof(structFields(u))
		var end int64
		for i := 0; i < u.NumFields(); i++ {
			ft := u.Field(i).Type()
			if offs[i] != end || !isRegularMemory(ft, sizes) {
				return false
			}
			end = offs[i] + sizes.Sizeof(ft)
		}
		return end == sizes.Sizeof(u)
	}
	return false
}

// runtimeTypeString formats t the way the gc runtime prints types.
func runtimeTypeString(t types.Type) string {
	var b strings.Builder
	writeRuntimeType(&b, t)
	return b.String()
}

func writeRuntimeType(b *strings.Builder, t types.Type) {
	switch u := types.Unalias(t).(type) {
	case *types.Basic:
		b.WriteString(u.Name())
		if u.Kind() == types.UnsafePointer {
			b.Reset()
			b.WriteString("unsafe.Pointer")
		}
	case *types.Named:
		if u.Obj().Pkg() != nil {
			b.WriteString(u.Obj().Pkg().Name())
			b.WriteByte('.')
		}
		b.WriteString(u.Obj().Name())
		if ta := u.TypeArgs(); ta != nil && ta.Len() > 0 {
			b.WriteByte('[')
			for i := 0; i < ta.Len(); i++ {
				if i > 0 {
					b.WriteByte(',')
				}
				writeRuntimeType(b, ta.At(i))
			}
			b.WriteByte(']')
		}
	case *types.Pointer:
		b.WriteByte('*')
		writeRuntimeType(b, u.Elem())
	case *types.Slice:
		b.WriteString("[]")
		writeRuntimeType(b, u.Elem())
	case *types.Array:
		fmt.Fprintf(b, "[%d]", u.Len())
		writeRuntimeType(b, u.Elem())
	case *types.Map:
		b.WriteString("map[")
		writeRuntimeType(b, u.Key())
		b.WriteByte(']')
		writeRuntimeType(b, u.Elem())
	case *types.Chan:
		switch u.Dir() {
		case types.SendRecv:
			b.WriteString("chan ")
			if c, ok := u.Elem().(*types.Chan); ok && c.Dir() == types.RecvOnly {
				b.WriteString("(")
				writeRuntimeType(b, u.Elem())
				b.WriteString(")")
				return
			}
		case types.SendOnly:
			b.WriteString("chan<- ")
		case types.RecvOnly:
			b.WriteString("<-chan ")
		}
		writeRuntimeType(b, u.Elem())
	case *types.Signature:
		b.WriteString("func")
		writeSig(b, u)
	case *types.Interface:
		if u.NumMethods() == 0 && u.NumEmbeddeds() == 0 {
			b.WriteString("interface {}")
			return
		}
		b.WriteString("interface {")
		for i, m := range sortedIfaceMethods(u) {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteByte(' ')
			if !m.Exported() && m.Pkg() != nil {
				b.WriteString(m.Pkg().Name() + ".")
			}
			b.WriteString(m.Name())
			writeSig(b, m.Type().(*types.Signature))
		}
		b.WriteString(" }")
	case *types.Struct:
		if u.NumFields() == 0 {
			b.WriteString("struct {}")
			return
		}
		b.WriteString("struct {")
		for i := 0; i < u.NumFields(); i++ {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteByte(' ')
			f := u.Field(i)
			if !f.Embedded() {
				b.WriteString(f.Name())
				b.WriteByte(' ')
			}
			writeRuntimeType(b, f.Type())
			if tag := u.Tag(i); tag != "" {
				fmt.Fprintf(b, " %q", tag)
			}
		}
		b.WriteString(" }")
	case *types.TypeParam:
		b.WriteString(u.Obj().Name())
	default:
		b.WriteString(t.String())
	}
}

func writeSig(b *strings.Builder, s *types.Signature) {
	b.WriteByte('(')
	for i := 0; i < s.Params().Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		pt := s.Params().At(i).Type()
		if s.Variadic() && i == s.Params().Len()-1 {
			b.WriteString("...")
			pt = pt.(*types.Slice).Elem()
		}
		writeRuntimeType(b, pt)
	}
	b.WriteByte(')')
	switch s.Results().Len() {
	case 0:
	case 1:
		b.WriteByte(' ')
		writeRuntimeType(b, s.Results().At(0).Type())
	default:
		b.WriteString(" (")
		for i := 0; i < s.Results().Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			writeRuntimeType(b, s.Results().At(i).Type())
		}
		b.WriteByte(')')
	}
}

var (
	_ = token.NoPos
	_ ssa.Value
)
