// Entry points for marmot's compile time fmt specialization (internal/specialize).
// The compiler rewrites fmt calls with a constant format string into straight line
// calls to these functions. Each one is the step doPrintf, doPrint or doPrintln would
// take for one verb or operand, so the output is the same by construction.
// User code must not call them.

package fmt

import (
	"errors"
	"io"
	"reflect"
)

// MarmotPrinter is the printer state the specialized code threads through.
type MarmotPrinter = pp

// Flag bits for MarmotFlags, in the order of fmtFlags.
const (
	MarmotWidPresent = 1 << iota
	MarmotPrecPresent
	MarmotMinus
	MarmotPlus
	MarmotSharp
	MarmotSpace
	MarmotZero
	MarmotPlusV
	MarmotSharpV
)

func MarmotNewPrinter() *pp { return newPrinter() }

// MarmotNewErrorPrinter is newPrinter as Errorf sets it up.
func MarmotNewErrorPrinter() *pp {
	p := newPrinter()
	p.wrapErrs = true
	return p
}

// MarmotFlags sets the flag state that doPrintf computed for one verb.
func MarmotFlags(p *pp, bits uint, wid, prec int) {
	p.fmt.fmtFlags = fmtFlags{
		widPresent:  bits&MarmotWidPresent != 0,
		precPresent: bits&MarmotPrecPresent != 0,
		minus:       bits&MarmotMinus != 0,
		plus:        bits&MarmotPlus != 0,
		sharp:       bits&MarmotSharp != 0,
		space:       bits&MarmotSpace != 0,
		zero:        bits&MarmotZero != 0,
		plusV:       bits&MarmotPlusV != 0,
		sharpV:      bits&MarmotSharpV != 0,
	}
	p.fmt.wid = wid
	p.fmt.prec = prec
}

// MarmotClear is clearflags, for operands of Print and Println.
func MarmotClear(p *pp) { p.fmt.clearflags() }

func MarmotLit(p *pp, s string) { p.buf.writeString(s) }

func MarmotByte(p *pp, c byte) { p.buf.writeByte(c) }

func MarmotInt(p *pp, v int64, verb rune) {
	p.fmtInteger(nil, reflect.Value{}, uint64(v), signed, verb)
}

func MarmotUint(p *pp, v uint64, verb rune) {
	p.fmtInteger(nil, reflect.Value{}, v, unsigned, verb)
}

func MarmotFloat(p *pp, v float64, size int, verb rune) {
	p.fmtFloat(nil, reflect.Value{}, v, size, verb)
}

func MarmotComplex(p *pp, v complex128, size int, verb rune) {
	p.fmtComplex(nil, reflect.Value{}, v, size, verb)
}

func MarmotString(p *pp, v string, verb rune) {
	p.fmtString(nil, reflect.Value{}, v, verb)
}

func MarmotBool(p *pp, v bool, verb rune) {
	p.fmtBool(nil, reflect.Value{}, v, verb)
}

func MarmotBytes(p *pp, v []byte, verb rune) {
	p.fmtBytes(v, verb, "[]byte")
}

// MarmotArg is the general path for one operand: printArg.
func MarmotArg(p *pp, a any, verb rune) { p.printArg(a, verb) }

func MarmotWrite(p *pp, w io.Writer) (n int, err error) {
	n, err = w.Write(p.buf)
	p.free()
	return
}

func MarmotSprint(p *pp) string {
	s := string(p.buf)
	p.free()
	return s
}

func MarmotAppend(p *pp, b []byte) []byte {
	b = append(b, p.buf...)
	p.free()
	return b
}

// MarmotErrorf finishes an Errorf whose format has no %w verb.
func MarmotErrorf(p *pp) error {
	s := string(p.buf)
	p.free()
	return errors.New(s)
}
