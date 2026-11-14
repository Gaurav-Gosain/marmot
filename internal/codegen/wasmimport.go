package codegen

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// wasmImport is a //go:wasmimport MODULE NAME directive on a body-less
// function.
type wasmImport struct{ module, name string }

// wasmImportWrapper defines fn, a body-less Go function with
// //go:wasmimport, as a wrapper that converts its arguments to the wasm
// import's types and calls the import (a private declaration carrying
// wasm-import-module and wasm-import-name). Callers in every package then
// call an ordinary Go function. The types follow gc: int32, uint32,
// int64, uint64, float32 and float64 as they are; bool as i32; uintptr,
// unsafe.Pointer and pointers as i32 (an address in the 32-bit memory);
// a string as its pointer and length (two i32).
func (m *moduleGen) wasmImportWrapper(fn *ssa.Function, sym string, wi wasmImport) {
	params, ret := m.signature(fn)
	imp := sym + "$wasmimport"
	var b strings.Builder
	var args, argTypes []string
	n := 0
	tmp := func() string { n++; return fmt.Sprintf("%%w%d", n) }
	fmt.Fprintf(&b, "  llvm.func %s(", mlir.Symbol(sym))
	for i, t := range params {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%%a%d: %s", i, t)
	}
	b.WriteString(")")
	if ret != mlir.Void {
		b.WriteString(" -> " + ret)
	}
	b.WriteString(" {\n")
	i := 0
	for v := range fn.Signature.Params().Variables() {
		a := fmt.Sprintf("%%a%d", i)
		t := params[i]
		i++
		switch wasmKind(v.Type()) {
		case "bool":
			r := tmp()
			fmt.Fprintf(&b, "    %s = llvm.zext %s : i1 to i32\n", r, a)
			args, argTypes = append(args, r), append(argTypes, mlir.I32)
		case "word":
			r := tmp()
			fmt.Fprintf(&b, "    %s = llvm.trunc %s : %s to i32\n", r, a, t)
			args, argTypes = append(args, r), append(argTypes, mlir.I32)
		case "string":
			p, l, l32 := tmp(), tmp(), tmp()
			fmt.Fprintf(&b, "    %s = llvm.extractvalue %s[0] : %s\n", p, a, t)
			fmt.Fprintf(&b, "    %s = llvm.extractvalue %s[1] : %s\n", l, a, t)
			fmt.Fprintf(&b, "    %s = llvm.trunc %s : i64 to i32\n", l32, l)
			args, argTypes = append(args, p, l32), append(argTypes, tP, mlir.I32)
		default: // as it is: fixed-size numbers and pointers
			args, argTypes = append(args, a), append(argTypes, t)
		}
	}
	var res types.Type
	if r := fn.Signature.Results(); r.Len() == 1 {
		res = r.At(0).Type()
	}
	iret := ret
	switch {
	case res == nil:
	case wasmKind(res) == "bool", wasmKind(res) == "word":
		iret = mlir.I32
	}
	call := fmt.Sprintf("llvm.call %s(%s) : (%s) -> %s", mlir.Symbol(imp), strings.Join(args, ", "), strings.Join(argTypes, ", "), orVoid(iret))
	if res == nil {
		fmt.Fprintf(&b, "    %s\n    llvm.return\n  }\n", call)
	} else {
		r := tmp()
		fmt.Fprintf(&b, "    %s = %s\n", r, call)
		switch wasmKind(res) {
		case "bool":
			z, c := tmp(), tmp()
			fmt.Fprintf(&b, "    %s = llvm.mlir.constant(0 : i32) : i32\n    %s = llvm.icmp \"ne\" %s, %s : i32\n", z, c, r, z)
			r = c
		case "word":
			e := tmp()
			fmt.Fprintf(&b, "    %s = llvm.zext %s : i32 to %s\n", e, r, ret)
			r = e
		}
		fmt.Fprintf(&b, "    llvm.return %s : %s\n  }\n", r, ret)
	}
	m.defs.WriteString(b.String())
	m.declareRaw(imp, argTypes, iret, fmt.Sprintf(`sym_visibility = "private", passthrough = [["wasm-import-module", %q], ["wasm-import-name", %q]]`, wi.module, wi.name))
}

// wasmKind classifies a wasmimport parameter or result type: "bool",
// "word" (uintptr and other 8-byte integers that are addresses or sizes
// in the 32-bit memory: uintptr only, as gc), "string", or "" (passed as
// it is).
func wasmKind(t types.Type) string {
	if b, ok := t.Underlying().(*types.Basic); ok {
		switch b.Kind() {
		case types.Bool:
			return "bool"
		case types.Uintptr:
			return "word"
		case types.String:
			return "string"
		}
	}
	return ""
}

func orVoid(t string) string {
	if t == mlir.Void {
		return "()"
	}
	return t
}

// cWord reports whether t is a Go int, uint or uintptr: 8 bytes in Go on
// wasm, the C runtime's native word (intptr_t, size_t, 4 bytes) at the C
// boundary.
func cWord(t types.Type) bool {
	if b, ok := t.Underlying().(*types.Basic); ok {
		switch b.Kind() {
		case types.Int, types.Uint, types.Uintptr:
			return true
		}
	}
	return false
}

// cWordSig reports whether a call of the C function fn passes or returns a
// Go word, which the C side has as an i32 on wasm.
func cWordSig(fn *ssa.Function) bool {
	sig := fn.Signature
	if sig.Recv() != nil || len(fn.FreeVars) > 0 {
		return false
	}
	for v := range sig.Params().Variables() {
		if cWord(v.Type()) {
			return true
		}
	}
	return sig.Results().Len() == 1 && cWord(sig.Results().At(0).Type())
}

// cWordWrapper makes the C function sym callable with fn's Go signature on
// wasm: it declares sym with C types (a Go word argument is an i32) and
// defines a linkonce_odr wrapper with the Go types that truncates word
// arguments and extends a word result (sign for int, zero for uint and
// uintptr, as intptr_t/ssize_t and size_t/uintptr_t). Lengths in a 32-bit
// memory fit in 32 bits, so the truncation loses nothing. It returns the
// wrapper's symbol.
func (m *moduleGen) cWordWrapper(sym string, fn *ssa.Function) string {
	w := sym + "$cw"
	if m.defined[w] {
		return w
	}
	m.defined[w] = true
	params, ret := m.signature(fn)
	sig := fn.Signature
	var b strings.Builder
	var args, cparams []string
	n := 0
	tmp := func() string { n++; return fmt.Sprintf("%%c%d", n) }
	fmt.Fprintf(&b, "  llvm.func linkonce_odr %s(", mlir.Symbol(w))
	for i, t := range params {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%%a%d: %s", i, t)
	}
	b.WriteString(")")
	if ret != mlir.Void {
		b.WriteString(" -> " + ret)
	}
	b.WriteString(" attributes {always_inline} {\n")
	var decl strings.Builder
	fmt.Fprintf(&decl, "  llvm.func %s(", mlir.Symbol(sym))
	for i, t := range params {
		a := fmt.Sprintf("%%a%d", i)
		ct := t
		if cWord(sig.Params().At(i).Type()) {
			r := tmp()
			fmt.Fprintf(&b, "    %s = llvm.trunc %s : %s to i32\n", r, a, t)
			a, ct = r, mlir.I32
		}
		args, cparams = append(args, a), append(cparams, ct)
		if i > 0 {
			decl.WriteString(", ")
		}
		decl.WriteString(ct + extAttr(t, fn, i))
	}
	decl.WriteString(")")
	cret := ret
	var res types.Type
	if sig.Results().Len() == 1 {
		res = sig.Results().At(0).Type()
		if cWord(res) {
			cret = mlir.I32
		}
	}
	if cret != mlir.Void {
		decl.WriteString(" -> " + cret)
	}
	if a := m.funcAttrs(fn, sym, true); a != "" {
		decl.WriteString(" attributes {" + a + "}")
	}
	decl.WriteString("\n")
	m.decls[sym] = decl.String()
	call := fmt.Sprintf("llvm.call %s(%s) : (%s) -> %s", mlir.Symbol(sym), strings.Join(args, ", "), strings.Join(cparams, ", "), orVoid(cret))
	switch {
	case cret == mlir.Void:
		fmt.Fprintf(&b, "    %s\n    llvm.return\n  }\n", call)
	case res != nil && cWord(res):
		r, e := tmp(), tmp()
		ext := "llvm.zext"
		if isSigned(res) {
			ext = "llvm.sext"
		}
		fmt.Fprintf(&b, "    %s = %s\n    %s = %s %s : i32 to %s\n    llvm.return %s : %s\n  }\n", r, call, e, ext, r, ret, e, ret)
	default:
		r := tmp()
		fmt.Fprintf(&b, "    %s = %s\n    llvm.return %s : %s\n  }\n", r, call, r, ret)
	}
	m.defs.WriteString(b.String())
	return w
}
