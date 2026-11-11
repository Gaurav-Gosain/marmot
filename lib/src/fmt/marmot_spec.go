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
	"strconv"
	"sync/atomic"
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

// marmotSlot holds one free printer in front of ppFree: Swap and
// CompareAndSwap are cheaper than the pool's pin and unpin, and which free
// printer is reused cannot be observed.
var marmotSlot atomic.Pointer[pp]

func MarmotNewPrinter() *pp {
	if p := marmotSlot.Swap(nil); p != nil {
		p.panicking = false
		p.erroring = false
		p.wrapErrs = false
		p.fmt.init(&p.buf)
		return p
	}
	return newPrinter()
}

// marmotFree is free with the slot tried first.
func (p *pp) marmotFree() {
	// Proper usage of a sync.Pool requires each entry to have approximately
	// the same memory cost. To obtain this property when the stored type
	// contains a variably-sized buffer, we add a hard limit on the maximum
	// buffer to place back in the pool. If the buffer is larger than the
	// limit, we drop the buffer and recycle just the printer.
	//
	// See https://golang.org/issue/23199.
	if cap(p.buf) > 64*1024 {
		p.buf = nil
	} else {
		p.buf = p.buf[:0]
	}
	if cap(p.wrappedErrs) > 8 {
		p.wrappedErrs = nil
	}

	p.wrappedErrs = p.wrappedErrs[:0]
	if !marmotSlot.CompareAndSwap(nil, p) {
		ppFree.Put(p)
	}
}

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
	p.marmotFree()
	return
}

func MarmotSprint(p *pp) string {
	s := string(p.buf)
	p.marmotFree()
	return s
}

func MarmotAppend(p *pp, b []byte) []byte {
	b = append(b, p.buf...)
	p.marmotFree()
	return b
}

// MarmotErrorf finishes an Errorf whose format has no %w verb.
func MarmotErrorf(p *pp) error {
	s := string(p.buf)
	p.marmotFree()
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
	p.fmt.marmotFmtIntegerASCII(u, base, isSigned, verb, digits)
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
	p.fmt.marmotFmtFloatASCII(v, size, verb, prec)
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

// ASCII padding variants, generated from the stock format.go: pad counts
// runes to measure the width; integer and float text is always ASCII, so
// these use the byte length. Same output.

func (f *fmt) marmotPadASCII(b []byte) {
	if !f.widPresent || f.wid == 0 {
		f.buf.write(b)
		return
	}
	width := f.wid - len(b)
	if !f.minus {
		// left padding
		f.writePadding(width)
		f.buf.write(b)
	} else {
		// right padding
		f.buf.write(b)
		f.writePadding(width)
	}
}

func (f *fmt) marmotFmtIntegerASCII(u uint64, base int, isSigned bool, verb rune, digits string) {
	negative := isSigned && int64(u) < 0
	if negative {
		u = -u
	}

	buf := f.intbuf[0:]
	// The already allocated f.intbuf with a capacity of 68 bytes
	// is large enough for integer formatting when no precision or width is set.
	if f.widPresent || f.precPresent {
		// Account 3 extra bytes for possible addition of a sign and "0x".
		width := 3 + f.wid + f.prec // wid and prec are always positive.
		if width > len(buf) {
			// We're going to need a bigger boat.
			buf = make([]byte, width)
		}
	}

	// Two ways to ask for extra leading zero digits: %.3d or %03d.
	// If both are specified the f.zero flag is ignored and
	// padding with spaces is used instead.
	prec := 0
	if f.precPresent {
		prec = f.prec
		// Precision of 0 and value of 0 means "print nothing" but padding.
		if prec == 0 && u == 0 {
			oldZero := f.zero
			f.zero = false
			f.writePadding(f.wid)
			f.zero = oldZero
			return
		}
	} else if f.zero && !f.minus && f.widPresent { // Zero padding is allowed only to the left.
		prec = f.wid
		if negative || f.plus || f.space {
			prec-- // leave room for sign
		}
	}

	// Because printing is easier right-to-left: format u into buf, ending at buf[i].
	// We could make things marginally faster by splitting the 32-bit case out
	// into a separate block but it's not worth the duplication, so u has 64 bits.
	i := len(buf)
	// Use constants for the division and modulo for more efficient code.
	// Switch cases ordered by popularity.
	switch base {
	case 10:
		for u >= 10 {
			i--
			next := u / 10
			buf[i] = byte('0' + u - next*10)
			u = next
		}
	case 16:
		for u >= 16 {
			i--
			buf[i] = digits[u&0xF]
			u >>= 4
		}
	case 8:
		for u >= 8 {
			i--
			buf[i] = byte('0' + u&7)
			u >>= 3
		}
	case 2:
		for u >= 2 {
			i--
			buf[i] = byte('0' + u&1)
			u >>= 1
		}
	default:
		panic("fmt: unknown base; can't happen")
	}
	i--
	buf[i] = digits[u]
	for i > 0 && prec > len(buf)-i {
		i--
		buf[i] = '0'
	}

	// Various prefixes: 0x, -, etc.
	if f.sharp {
		switch base {
		case 2:
			// Add a leading 0b.
			i--
			buf[i] = 'b'
			i--
			buf[i] = '0'
		case 8:
			if buf[i] != '0' {
				i--
				buf[i] = '0'
			}
		case 16:
			// Add a leading 0x or 0X.
			i--
			buf[i] = digits[16]
			i--
			buf[i] = '0'
		}
	}
	if verb == 'O' {
		i--
		buf[i] = 'o'
		i--
		buf[i] = '0'
	}

	if negative {
		i--
		buf[i] = '-'
	} else if f.plus {
		i--
		buf[i] = '+'
	} else if f.space {
		i--
		buf[i] = ' '
	}

	// Left padding with zeros has already been handled like precision earlier
	// or the f.zero flag is ignored due to an explicitly set precision.
	oldZero := f.zero
	f.zero = false
	f.marmotPadASCII(buf[i:])
	f.zero = oldZero
}

func (f *fmt) marmotFmtFloatASCII(v float64, size int, verb rune, prec int) {
	// Explicit precision in format specifier overrules default precision.
	if f.precPresent {
		prec = f.prec
	}
	// Format number, reserving space for leading + sign if needed.
	num := strconv.AppendFloat(f.intbuf[:1], v, byte(verb), prec, size)
	if num[1] == '-' || num[1] == '+' {
		num = num[1:]
	} else {
		num[0] = '+'
	}
	// f.space means to add a leading space instead of a "+" sign unless
	// the sign is explicitly asked for by f.plus.
	if f.space && num[0] == '+' && !f.plus {
		num[0] = ' '
	}
	// Special handling for infinities and NaN,
	// which don't look like a number so shouldn't be padded with zeros.
	if num[1] == 'I' || num[1] == 'N' {
		oldZero := f.zero
		f.zero = false
		// Remove sign before NaN if not asked for.
		if num[1] == 'N' && !f.space && !f.plus {
			num = num[1:]
		}
		f.marmotPadASCII(num)
		f.zero = oldZero
		return
	}
	// The sharp flag forces printing a decimal point for non-binary formats
	// and retains trailing zeros, which we may need to restore.
	if f.sharp && verb != 'b' {
		digits := 0
		switch verb {
		case 'v', 'g', 'G', 'x':
			digits = prec
			// If no precision is set explicitly use a precision of 6.
			if digits == -1 {
				digits = 6
			}
		}

		// Buffer pre-allocated with enough room for
		// exponent notations of the form "e+123" or "p-1023".
		var tailBuf [6]byte
		tail := tailBuf[:0]

		hasDecimalPoint := false
		sawNonzeroDigit := false
		// Starting from i = 1 to skip sign at num[0].
		for i := 1; i < len(num); i++ {
			switch num[i] {
			case '.':
				hasDecimalPoint = true
			case 'p', 'P':
				tail = append(tail, num[i:]...)
				num = num[:i]
			case 'e', 'E':
				if verb != 'x' && verb != 'X' {
					tail = append(tail, num[i:]...)
					num = num[:i]
					break
				}
				fallthrough
			default:
				if num[i] != '0' {
					sawNonzeroDigit = true
				}
				// Count significant digits after the first non-zero digit.
				if sawNonzeroDigit {
					digits--
				}
			}
		}
		if !hasDecimalPoint {
			// Leading digit 0 should contribute once to digits.
			if len(num) == 2 && num[1] == '0' {
				digits--
			}
			num = append(num, '.')
		}
		for digits > 0 {
			num = append(num, '0')
			digits--
		}
		num = append(num, tail...)
	}
	// We want a sign if asked for and if the sign is not positive.
	if f.plus || num[0] != '+' {
		// If we're zero padding to the left we want the sign before the leading zeros.
		// Achieve this by writing the sign out and then padding the unsigned number.
		// Zero padding is allowed only to the left.
		if f.zero && !f.minus && f.widPresent && f.wid > len(num) {
			f.buf.writeByte(num[0])
			f.writePadding(f.wid - len(num))
			f.buf.write(num[1:])
			return
		}
		f.marmotPadASCII(num)
		return
	}
	// No sign to show and the number is positive; just print the unsigned number.
	f.marmotPadASCII(num[1:])
}
