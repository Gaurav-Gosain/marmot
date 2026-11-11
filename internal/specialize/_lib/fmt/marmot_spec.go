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

const marmotDigits = "00010203040506070809101112131415161718192021222324252627282930313233343536373839404142434445464748495051525354555657585960616263646566676869707172737475767778798081828384858687888990919293949596979899"

// MarmotUintPlain writes u in decimal: what fmtInteger writes for %d with
// no flags, width or precision.
func MarmotUintPlain(p *pp, u uint64) {
	n := 1
	for x := u; x >= 10; x /= 10 {
		n++
	}
	b := []byte(p.buf)
	l := len(b)
	if cap(b)-l < n {
		b = append(b, make([]byte, n)...)
	} else {
		b = b[:l+n]
	}
	i := l + n
	for u >= 100 {
		q := u / 100
		r := (u - q*100) * 2
		i -= 2
		b[i] = marmotDigits[r]
		b[i+1] = marmotDigits[r+1]
		u = q
	}
	if u >= 10 {
		i -= 2
		b[i] = marmotDigits[u*2]
		b[i+1] = marmotDigits[u*2+1]
	} else {
		i--
		b[i] = byte('0' + u)
	}
	p.buf = buffer(b)
}

// MarmotIntPlain is MarmotUintPlain for signed values.
func MarmotIntPlain(p *pp, v int64) {
	if v < 0 {
		p.buf.writeByte('-')
		MarmotUintPlain(p, uint64(-v))
		return
	}
	MarmotUintPlain(p, uint64(v))
}

// Per verb entry points: the arm of pp.fmtInteger, pp.fmtFloat,
// pp.fmtString or pp.fmtBool that the verb selects, chosen at compile time,
// so no badVerb (and no reflection) is reachable from specialized code.

// MarmotFmtInteger is fmt.fmtInteger (verbs v, d, b, o, O, x, X).
func MarmotFmtInteger(p *pp, u uint64, base int, isSigned bool, verb rune, upper bool) {
	digits := ldigits
	if upper {
		digits = udigits
	}
	p.fmt.fmtInteger(u, base, isSigned, verb, digits)
}

// MarmotFmt0x64 is pp.fmt0x64 (%#v of unsigned integers).
func MarmotFmt0x64(p *pp, u uint64) { p.fmt0x64(u, true) }

// MarmotFmtC is %c of an integer.
func MarmotFmtC(p *pp, u uint64) { p.fmt.fmtC(u) }

// MarmotFmtQc is %q of an integer.
func MarmotFmtQc(p *pp, u uint64) { p.fmt.fmtQc(u) }

// MarmotFmtUnicode is %U of an integer.
func MarmotFmtUnicode(p *pp, u uint64) { p.fmt.fmtUnicode(u) }

// MarmotFmtFloat is fmt.fmtFloat with the verb and precision pp.fmtFloat
// picks.
func MarmotFmtFloat(p *pp, v float64, size int, verb rune, prec int) {
	p.fmt.fmtFloat(v, size, verb, prec)
}

// MarmotFmtComplex is pp.fmtComplex for a verb it accepts, with the float
// verb and precision already resolved.
func MarmotFmtComplex(p *pp, v complex128, size int, verb rune, prec int) {
	oldPlus := p.fmt.plus
	p.buf.writeByte('(')
	p.fmt.fmtFloat(real(v), size/2, verb, prec)
	p.fmt.plus = true
	p.fmt.fmtFloat(imag(v), size/2, verb, prec)
	p.buf.writeString("i)")
	p.fmt.plus = oldPlus
}

// MarmotFmtS is %s (and %v) of a string.
func MarmotFmtS(p *pp, s string) { p.fmt.fmtS(s) }

// MarmotFmtSx is %x or %X of a string.
func MarmotFmtSx(p *pp, s string, upper bool) {
	digits := ldigits
	if upper {
		digits = udigits
	}
	p.fmt.fmtSx(s, digits)
}

// MarmotFmtQ is %q (and %#v) of a string.
func MarmotFmtQ(p *pp, s string) { p.fmt.fmtQ(s) }

// MarmotFmtBoolean is %t and %v of a bool.
func MarmotFmtBoolean(p *pp, b bool) { p.fmt.fmtBoolean(b) }
