// Printing of panic values, in the gc runtime's format. Pure file.

package runtime

import "unsafe"

type stringer interface {
	String() string
}

// panicValueString converts error and Stringer panic values to their
// message before printing, as the gc runtime does. Runtime errors are
// handled without an interface call.
func panicValueString(v any) (string, bool) {
	switch x := v.(type) {
	case errorString:
		return x.Error(), true
	case plainError:
		return x.Error(), true
	case boundsError:
		return x.Error(), true
	case *TypeAssertionError:
		return x.Error(), true
	case *PanicNilError:
		return x.Error(), true
	case error:
		return x.Error(), true
	case stringer:
		return x.String(), true
	}
	return "", false
}

// printpanicval prints a panic value the way the gc runtime does: values
// of predeclared basic types plainly, strings with indented newlines, and
// everything else through printanycustomtype. It dispatches on the
// descriptor instead of a type switch, so that it does not force the
// descriptors of all basic types into every binary.
func printpanicval(v any) {
	e := efaceOf(&v)
	t := e.typ
	if t == nil {
		printstring("nil")
		return
	}
	if t.kind == kindString && t.str == "string" {
		printindented(*(*string)(e.data))
		return
	}
	if isPredeclaredBasic(t) {
		printbasic(t, e.data)
		return
	}
	printanycustomtype(v)
}

// basicNames are the type strings of the predeclared basic types, indexed
// by kind.
var basicNames = [...]string{
	kindBool:       "bool",
	kindInt:        "int",
	kindInt8:       "int8",
	kindInt16:      "int16",
	kindInt32:      "int32",
	kindInt64:      "int64",
	kindUint:       "uint",
	kindUint8:      "uint8",
	kindUint16:     "uint16",
	kindUint32:     "uint32",
	kindUint64:     "uint64",
	kindUintptr:    "uintptr",
	kindFloat32:    "float32",
	kindFloat64:    "float64",
	kindComplex64:  "complex64",
	kindComplex128: "complex128",
}

// isPredeclaredBasic reports whether t is one of the predeclared boolean
// or numeric types (not a defined type based on one).
func isPredeclaredBasic(t *_type) bool {
	return t.kind >= kindBool && t.kind <= kindComplex128 && t.str == basicNames[t.kind]
}

// printbasic prints the boolean or numeric value of type t at p.
func printbasic(t *_type, p unsafe.Pointer) {
	k := t.kind
	switch {
	case k == kindBool:
		printbool(*(*bool)(p))
	case k <= kindInt64:
		printint(loadInt(p, t.size))
	case k <= kindUintptr:
		printuint(loadUint(p, t.size))
	case k == kindFloat32:
		printfloat32(*(*float32)(p))
	case k == kindFloat64:
		printfloat64(*(*float64)(p))
	case k == kindComplex64:
		printcomplex64(*(*complex64)(p))
	default:
		printcomplex128(*(*complex128)(p))
	}
}

// printanycustomtype prints values of defined types with a basic underlying
// type as T(v) (complex values as T(re+imi)), and anything else as
// (T) address.
func printanycustomtype(v any) {
	e := efaceOf(&v)
	ts := e.typ.str
	p := e.data
	k := e.typ.kind
	switch {
	case k == kindString:
		printstring(ts)
		printstring(`("`)
		printindented(*(*string)(p))
		printstring(`")`)
	case k == kindComplex64 || k == kindComplex128:
		printstring(ts)
		printbasic(e.typ, p)
	case k >= kindBool && k <= kindFloat64:
		printstring(ts)
		printstring("(")
		printbasic(e.typ, p)
		printstring(")")
	default:
		printstring("(")
		printstring(ts)
		printstring(") ")
		printpointer(p)
	}
}

// loadInt loads a signed integer of size bytes.
func loadInt(p unsafe.Pointer, size uintptr) int64 {
	switch size {
	case 1:
		return int64(*(*int8)(p))
	case 2:
		return int64(*(*int16)(p))
	case 4:
		return int64(*(*int32)(p))
	}
	return *(*int64)(p)
}

// loadUint loads an unsigned integer of size bytes.
func loadUint(p unsafe.Pointer, size uintptr) uint64 {
	switch size {
	case 1:
		return uint64(*(*uint8)(p))
	case 2:
		return uint64(*(*uint16)(p))
	case 4:
		return uint64(*(*uint32)(p))
	}
	return *(*uint64)(p)
}

// printindented prints s, replacing "\n" with "\n\t".
func printindented(s string) {
	for {
		i := 0
		for i < len(s) && s[i] != '\n' {
			i++
		}
		if i == len(s) {
			break
		}
		printstring(s[:i+1])
		printstring("\t")
		s = s[i+1:]
	}
	printstring(s)
}

// _panic is an active panic. It lives in the frame of gopanic.
type _panic struct {
	arg  any
	link *_panic
	// sp is a stack address in the frame of the gopanic (or goexit) call
	// that owns this record. The record itself may be heap allocated, so
	// recovery compares sp, not the record's address, with frame
	// addresses.
	sp uintptr
	// deferfp is the frame address of the code that calls deferred
	// functions for this panic. recover compares against it.
	deferfp uintptr
	// wrapped reports whether the deferred call being run is a compiler
	// generated wrapper around a direct call (defer f(args)).
	wrapped bool
	// wrapfp is the frame address of the compiler wrapper (bound method
	// value, value receiver interface method, ...) running as, or called
	// directly by, the deferred function, so that the method it forwards
	// to may recover. 0 when none. See recoverWrapEnter in panic.go.
	wrapfp    uintptr
	recovered bool
	aborted   bool
	goexit    bool
	// repanicked marks a panic whose value was recovered and panicked
	// again unchanged.
	repanicked bool
	// msg is the result of the Error or String method of arg, set by
	// preprintpanics when hasMsg is true.
	msg    string
	hasMsg bool
}

// printpanics prints the chain of active panics, oldest first. Values
// must already be converted with panicValueString.
func printpanics(p *_panic) {
	if p.link != nil {
		printpanics(p.link)
		if p.link.repanicked {
			return
		}
		if !p.link.goexit {
			printstring("\t")
		}
	}
	if p.goexit {
		return
	}
	printstring("panic: ")
	if p.hasMsg {
		printindented(p.msg)
	} else {
		printpanicval(p.arg)
	}
	if p.recovered && p.repanicked {
		printstring(" [recovered, repanicked]")
	} else if p.recovered {
		printstring(" [recovered]")
	}
	printstring("\n")
	// A failed cgo pointer check says where, under gc's message.
	if e, ok := p.arg.(cgoPointerError); ok && e.detail != "" {
		printindented("\t" + e.detail)
		printstring("\n")
	}
}

// preprintpanics converts the panic values to strings before printing,
// so that Error and String methods run before the print lock is taken.
func preprintpanics(p *_panic) {
	for p != nil {
		if p.link != nil && *efaceOf(&p.link.arg) == *efaceOf(&p.arg) {
			// The same value as the next panic in the chain: mark it
			// repanicked and print it once.
			p.link.repanicked = true
			p = p.link
			continue
		}
		if s, ok := panicValueString(p.arg); ok {
			p.msg = s
			p.hasMsg = true
		}
		p = p.link
	}
}

// fatalpanic prints the panic chain and the goroutine header and exits
// with status 2.
func fatalpanic(p *_panic, goid int64) {
	gp := getg()
	if gp.printingPanic {
		// An Error or String method that preprintpanics called panicked
		// and nothing recovered it, as gc reports it.
		const text = "panic while printing panic value"
		switch r := p.arg.(type) {
		case string:
			throw(text + ": " + r)
		default:
			throw(text + ": type " + efaceOf(&p.arg).typ.str)
		}
	}
	gp.printingPanic = true
	preprintpanics(p)
	switch panicMode() {
	case panicJSON:
		printlock()
		printPanicJSON(p, goid, getcallerfp())
		printunlock()
		exit(2)
	case panicHuman:
		printlock()
		printPanicHuman(p, goid, getcallerfp())
		printunlock()
		exit(2)
	}
	printlock()
	printpanics(p)
	printstring("\ngoroutine ")
	printint(goid)
	printstring(" [running]:\n")
	printTraceback(getcallerfp())
	printunlock()
	exit(2)
}
