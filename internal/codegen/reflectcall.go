package codegen

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Adapters for reflect.Value.Call (lib/src/runtime/type.go, functype.call
// and uncommontype.tfns). They exist only when the program can reach
// reflect.Value.Call.

// newAdapterGen returns a function generator for compiler written helpers.
func (m *moduleGen) newAdapterGen() *funcGen {
	f := &funcGen{m: m, tm: m.tm, vals: map[ssa.Value]string{}, blkName: map[*ssa.BasicBlock]string{}}
	m.initBufs(f)
	return f
}

func (f *funcGen) finishAdapter(head string) string {
	var out textBuf
	out.WriteString(head)
	f.flushBufs(&out)
	out.WriteString("  }\n")
	return out.String()
}

func tupleStruct(t *types.Tuple) *types.Struct {
	fs := make([]*types.Var, t.Len())
	for i := range fs {
		fs[i] = types.NewField(token.NoPos, nil, fmt.Sprintf("X%d", i), t.At(i).Type(), false)
	}
	return types.NewStruct(fs, nil)
}

// callAdapter returns void call(closure, args, results) for signature sig:
// it loads the arguments from a Go struct of the parameter types, calls the
// func value, and stores the results into a Go struct of the result types.
func (m *moduleGen) callAdapter(sig *types.Signature) string {
	sym := "reflectcall:" + m.p.canonical(sig)
	if m.defined[sym] {
		return sym
	}
	m.defined[sym] = true
	f := m.newAdapterGen()
	ps, rs := tupleStruct(sig.Params()), tupleStruct(sig.Results())
	pmt, rmt := m.tm.memType(ps), m.tm.memType(rs)
	var args []string
	for i := 0; i < sig.Params().Len(); i++ {
		t := sig.Params().At(i).Type()
		args = append(args, f.load(f.gep("%args", pmt, 0, i), t))
	}
	args = append(args, "%fv")
	code := f.op("llvm.load %%fv {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr")
	params, ret := m.sigTypes(sig, true)
	r := f.indirectCall(code, args, params, ret)
	for j := 0; j < sig.Results().Len(); j++ {
		t := sig.Results().At(j).Type()
		v := r
		if sig.Results().Len() > 1 {
			v = f.extractRaw(r, ret, j)
		}
		f.store(v, f.gep("%res", rmt, 0, j), t)
	}
	f.emit("llvm.return")
	head := fmt.Sprintf("  llvm.func linkonce_odr %s(%%fv: !llvm.ptr, %%args: !llvm.ptr, %%res: !llvm.ptr) attributes {no_unwind, passthrough = [[\"frame-pointer\", \"non-leaf\"]]} {\n", mlir.Symbol(sym))
	m.defs.WriteString(f.finishAdapter(head))
	return sym
}

// methodValueWrapper returns the tfn for a method whose itab function is
// ifn: a func value body that takes the method's arguments and the closure
// {tfn, recv}, and calls ifn(recv, args...).
func (m *moduleGen) methodValueWrapper(ifn string, method *types.Func) string {
	sym := ifn + "$tfn"
	if m.defined[sym] {
		return sym
	}
	m.defined[sym] = true
	sig := stripRecv(method.Type().(*types.Signature))
	params, ret := m.sigTypes(sig, false)
	f := m.newAdapterGen()
	recv := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", f.gepByte("%ctx", 8))
	var head strings.Builder
	fmt.Fprintf(&head, "  llvm.func linkonce_odr %s(", mlir.Symbol(sym))
	args := []string{recv}
	for i, t := range params {
		fmt.Fprintf(&head, "%%a%d: %s, ", i, t)
		args = append(args, fmt.Sprintf("%%a%d", i))
	}
	head.WriteString("%ctx: !llvm.ptr)")
	if ret != "" {
		head.WriteString(" -> " + ret)
	}
	head.WriteString(" attributes {no_unwind, passthrough = [[\"frame-pointer\", \"non-leaf\"]]} {\n")
	code := f.addressOf(ifn)
	r := f.indirectCall(code, args, append([]string{mlir.Ptr}, params...), ret)
	if ret == "" {
		f.emit("llvm.return")
	} else {
		f.emit("llvm.return %s : %s", r, ret)
	}
	m.defs.WriteString(f.finishAdapter(head.String()))
	return sym
}
