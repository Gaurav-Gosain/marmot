package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
)

// cv is a constant value used to initialize globals (type descriptors,
// itabs, closures). It is written out as an initializer region.
type cv struct {
	typ   string // LLVM type
	kind  cvKind
	i     int64
	sym   string
	f     float64
	img   *memImage
	elems []cv
}

type cvKind uint8

const (
	cvInt cvKind = iota
	cvZero
	cvAddr
	cvAgg
	cvFloat
	cvImage // static init only: resolved before emission
)

func cInt(v int64, typ string) cv { return cv{typ: typ, kind: cvInt, i: v} }
func cI64(v int64) cv             { return cInt(v, mlir.I64) }
func cNull() cv                   { return cv{typ: mlir.Ptr, kind: cvZero} }
func cAddr(sym string) cv         { return cv{typ: mlir.Ptr, kind: cvAddr, sym: sym} }
func cZero(typ string) cv         { return cv{typ: typ, kind: cvZero} }

func cAgg(typ string, elems ...cv) cv {
	return cv{typ: typ, kind: cvAgg, elems: elems}
}

// cStruct builds a literal struct value from its elements' types.
func cStruct(elems ...cv) cv {
	ts := make([]string, len(elems))
	for i, e := range elems {
		ts[i] = e.typ
	}
	return cAgg(mlir.Struct(ts...), elems...)
}

func cArray(elemType string, elems ...cv) cv {
	return cAgg(mlir.Array(int64(len(elems)), elemType), elems...)
}

// cString makes a Go string constant in module m for use inside global
// initializers. Its bytes live in a linkonce_odr global named by content,
// so linkonce data (descriptors) never refers to a module-private symbol.
func (m *moduleGen) cString(s string) cv {
	if s == "" {
		return cZero(tString)
	}
	return cAgg(tString, cAddr(m.sharedStringData(s)), cI64(int64(len(s))))
}

// sharedStringData returns a linkonce_odr global holding s.
func (m *moduleGen) sharedStringData(s string) string {
	h := sha256.Sum256([]byte(s))
	sym := "go:str." + hex.EncodeToString(h[:12])
	if !m.defined[sym] {
		m.defined[sym] = true
		fmt.Fprintf(&m.defs, "  llvm.mlir.global linkonce_odr constant %s(%s) {addr_space = 0 : i32}\n",
			mlir.Symbol(sym), mlir.Quote(s))
	}
	return sym
}

// constGlobal defines a global initialized with c.
func (m *moduleGen) constGlobal(sym, linkage string, constant bool, c cv) {
	m.defined[sym] = true
	b := &m.defs
	k := ""
	if constant {
		k = "constant "
	}
	var init []byte
	if init = appendInit(init, c); init == nil {
		fmt.Fprintf(b, "  llvm.mlir.global %s %s%s() {addr_space = 0 : i32} : %s {\n", linkage, k, mlir.Symbol(sym), c.typ)
		n := 0
		v := writeCV(b, &n, c)
		fmt.Fprintf(b, "    llvm.return %s : %s\n  }\n", v, c.typ)
		return
	}
	fmt.Fprintf(b, "  llvm.mlir.global %s %s%s() {addr_space = 0 : i32, go.init = %s} : %s\n",
		linkage, k, mlir.Symbol(sym), mlir.Quote(string(init)), c.typ)
}

// appendInit appends c in the go.init form, which the backend expands into
// the initializer region writeCV writes (a type descriptor's region is about
// 36 ops of text; parsing them was a third of a cold build's parse time).
// The backend takes each value's type from the enclosing aggregate (an
// element's type is the aggregate's element type, as the insertvalue ops
// of the region need); nil if c holds a value the form has no letter for,
// and the caller writes the region.
//
//	i<decimal>;  integer      f<16 hex digits>  float (bits of a double)
//	z            zero         a<len>:<symbol>   address of symbol
//	( ... )      aggregate, one value per element
func appendInit(out []byte, c cv) []byte {
	switch c.kind {
	case cvInt:
		if c.typ == mlir.F32 || c.typ == mlir.F64 {
			return appendFloatInit(out, float64(c.i), c.typ)
		}
		out = append(out, 'i')
		out = strconv.AppendInt(out, c.i, 10)
		return append(out, ';')
	case cvFloat:
		return appendFloatInit(out, c.f, c.typ)
	case cvZero:
		return append(out, 'z')
	case cvAddr:
		out = append(out, 'a')
		out = strconv.AppendInt(out, int64(len(c.sym)), 10)
		out = append(out, ':')
		return append(out, c.sym...)
	case cvAgg:
		out = append(out, '(')
		for _, e := range c.elems {
			if out = appendInit(out, e); out == nil {
				return nil
			}
		}
		return append(out, ')')
	}
	return nil
}

func appendFloatInit(out []byte, f float64, typ string) []byte {
	if typ == mlir.F32 {
		f = float64(float32(f))
	} else if typ != mlir.F64 {
		return nil
	}
	out = append(out, 'f')
	h := strconv.FormatUint(math.Float64bits(f), 16)
	for i := len(h); i < 16; i++ {
		out = append(out, '0')
	}
	return append(out, h...)
}

// cvWriter is where writeCV writes.
type cvWriter interface {
	io.Writer
	WriteString(string) (int, error)
}

func writeCV(b cvWriter, n *int, c cv) string {
	name := func() string {
		*n++
		return fmt.Sprintf("%%c%d", *n)
	}
	switch c.kind {
	case cvInt:
		v := name()
		if c.typ == mlir.F32 || c.typ == mlir.F64 {
			fmt.Fprintf(b, "    %s = llvm.mlir.constant(%s : %s) : %s\n", v, mlir.FloatAttr(float64(c.i), c.typ), c.typ, c.typ)
		} else {
			fmt.Fprintf(b, "    %s = llvm.mlir.constant(%s : %s) : %s\n", v, mlir.IntAttr(uint64(c.i), intBits(c.typ)), c.typ, c.typ)
		}
		return v
	case cvFloat:
		v := name()
		fmt.Fprintf(b, "    %s = llvm.mlir.constant(%s : %s) : %s\n", v, mlir.FloatAttr(c.f, c.typ), c.typ, c.typ)
		return v
	case cvZero:
		v := name()
		fmt.Fprintf(b, "    %s = llvm.mlir.zero : %s\n", v, c.typ)
		return v
	case cvAddr:
		v := name()
		fmt.Fprintf(b, "    %s = llvm.mlir.addressof %s : !llvm.ptr\n", v, mlir.Symbol(c.sym))
		return v
	case cvAgg:
		vals := make([]string, len(c.elems))
		for i, e := range c.elems {
			vals[i] = writeCV(b, n, e)
		}
		agg := name()
		fmt.Fprintf(b, "    %s = llvm.mlir.undef : %s\n", agg, c.typ)
		for i, v := range vals {
			next := name()
			fmt.Fprintf(b, "    %s = llvm.insertvalue %s, %s[%d] : %s\n", next, v, agg, i, c.typ)
			agg = next
		}
		return agg
	}
	panic("writeCV")
}
