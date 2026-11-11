// Print functions for the print and println builtins.
//
// The compiler lowers println(a, b) to
//
//	printlock(); printint(a); printsp(); printstring(b); printnl(); printunlock()
//
// Output goes to standard error. Between printlock and printunlock output is
// collected in a buffer and written with as few write calls as possible, so
// lines from different goroutines do not interleave. The formats match the gc
// toolchain exactly.
//
// Pure file: depends only on the primitives (see prim_marmot.go), plus
// appendFloat and appendComplex (internal/strconv under marmot, strconv on
// the host).

package runtime

import "unsafe"

var (
	printMu  mutex
	printBuf [512]byte
	printLen int
)

// printlock may be called recursively by the same goroutine.
func printlock() {
	gp := getg()
	if gp.printlock == 0 {
		lock(&printMu)
	}
	gp.printlock++
}

func printunlock() {
	gp := getg()
	gp.printlock--
	if gp.printlock == 0 {
		printflush()
		unlock(&printMu)
	}
}

// printflush writes out buffered output. Called with printMu held.
func printflush() {
	if printLen > 0 {
		writeErr(unsafe.Pointer(&printBuf[0]), uintptr(printLen))
		printLen = 0
	}
}

// gwrite writes n bytes at p to standard error, through the buffer when the
// calling goroutine holds the print lock.
func gwrite(p unsafe.Pointer, n int) {
	if n == 0 {
		return
	}
	gp := getg()
	if gp.printlock == 0 {
		writeErr(p, uintptr(n))
		return
	}
	if printLen+n > len(printBuf) {
		printflush()
		if n > len(printBuf) {
			writeErr(p, uintptr(n))
			return
		}
	}
	memmove(unsafe.Pointer(&printBuf[printLen]), p, uintptr(n))
	printLen += n
}

func gwriteBytes(b []byte) {
	if len(b) > 0 {
		gwrite(unsafe.Pointer(&b[0]), len(b))
	}
}

func printsp() { printstring(" ") }

func printnl() { printstring("\n") }

func printstring(s string) {
	gwrite(unsafe.Pointer(unsafe.StringData(s)), len(s))
}

func printbool(v bool) {
	if v {
		printstring("true")
	} else {
		printstring("false")
	}
}

// printfloat64 prints v in the shortest form that reads back exactly, as
// strconv.FormatFloat(v, 'g', -1, 64) does.
func printfloat64(v float64) {
	var buf [24]byte
	gwriteBytes(appendFloat(buf[:0], v, 64))
}

// printfloat32 prints v as strconv.FormatFloat(float64(v), 'g', -1, 32).
func printfloat32(v float32) {
	var buf [15]byte
	gwriteBytes(appendFloat(buf[:0], float64(v), 32))
}

// printfloat is printfloat64 (the name used by early compiler versions).
func printfloat(v float64) { printfloat64(v) }

// printcomplex128 prints c as (re+imi) with shortest floats.
func printcomplex128(c complex128) {
	var buf [51]byte
	gwriteBytes(appendComplex(buf[:0], c, 128))
}

// printcomplex64 prints c with float32 precision.
func printcomplex64(c complex64) {
	var buf [33]byte
	gwriteBytes(appendComplex(buf[:0], complex128(c), 64))
}

// printcomplex is printcomplex128 (the name used by early compiler versions).
func printcomplex(c complex128) { printcomplex128(c) }

func printuint(v uint64) {
	var buf [20]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte(v%10 + '0')
		v /= 10
		if v == 0 {
			break
		}
	}
	gwriteBytes(buf[i:])
}

func printint(v int64) {
	if v < 0 {
		printstring("-")
		v = -v
	}
	printuint(uint64(v))
}

var minhexdigits = 0 // protected by printlock

func printhexopts(include0x bool, mindigits int, v uint64) {
	const dig = "0123456789abcdef"
	var buf [100]byte
	i := len(buf)
	for i--; i > 0; i-- {
		buf[i] = dig[v%16]
		if v < 16 && len(buf)-i >= mindigits {
			break
		}
		v /= 16
	}
	if include0x {
		i--
		buf[i] = 'x'
		i--
		buf[i] = '0'
	}
	gwriteBytes(buf[i:])
}

func printhex(v uint64) {
	printhexopts(true, minhexdigits, v)
}

// printquoted prints s as a Go quoted string, escaping everything that is
// not printable ASCII.
func printquoted(s string) {
	printlock()
	printstring(`"`)
	for i := 0; i < len(s); {
		r := rune(s[i])
		n := 1
		if r >= runeSelf {
			var next int
			r, next = decoderune(s, i)
			n = next - i
		}
		switch r {
		case '\n':
			printstring(`\n`)
		case '\r':
			printstring(`\r`)
		case '\t':
			printstring(`\t`)
		case '\\':
			printstring(`\\`)
		case '"':
			printstring(`\"`)
		default:
			if r == runeError && n == 1 {
				// An invalid byte, not an encoded U+FFFD.
				printstring(`\x`)
				printhexopts(false, 2, uint64(s[i]))
			} else if r >= ' ' && r <= '~' {
				gwrite(unsafe.Pointer(unsafe.StringData(s[i:])), 1)
			} else if r < 127 {
				printstring(`\x`)
				printhexopts(false, 2, uint64(r))
			} else if r < 0x1_0000 {
				printstring(`\u`)
				printhexopts(false, 4, uint64(r))
			} else {
				printstring(`\U`)
				printhexopts(false, 8, uint64(r))
			}
		}
		i += n
	}
	printstring(`"`)
	printunlock()
}

func printpointer(p unsafe.Pointer) {
	printhex(uint64(uintptr(p)))
}

func printuintptr(p uintptr) {
	printhex(uint64(p))
}

// printeface prints an interface{} value as (type word,data word).
func printeface(e eface) {
	printstring("(")
	printpointer(unsafe.Pointer(e.typ))
	printstring(",")
	printpointer(e.data)
	printstring(")")
}

// printiface prints a non-empty interface value as (itab word,data word).
func printiface(i iface) {
	printstring("(")
	printpointer(unsafe.Pointer(i.tab))
	printstring(",")
	printpointer(i.data)
	printstring(")")
}

// printslice prints a slice as [len/cap]array.
func printslice(array unsafe.Pointer, len, cap int) {
	printstring("[")
	printint(int64(len))
	printstring("/")
	printint(int64(cap))
	printstring("]")
	printpointer(array)
}

// printstringNoLock prints s without taking the print lock; used by fatal
// error paths.
func printstringNoLock(s string) {
	writeErr(unsafe.Pointer(unsafe.StringData(s)), uintptr(len(s)))
}
