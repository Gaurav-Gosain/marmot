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

// printpanicval prints a panic value the way the gc runtime does.
func printpanicval(v any) {
	switch x := v.(type) {
	case nil:
		printstring("nil")
	case bool:
		printbool(x)
	case int:
		printint(int64(x))
	case int8:
		printint(int64(x))
	case int16:
		printint(int64(x))
	case int32:
		printint(int64(x))
	case int64:
		printint(x)
	case uint:
		printuint(uint64(x))
	case uint8:
		printuint(uint64(x))
	case uint16:
		printuint(uint64(x))
	case uint32:
		printuint(uint64(x))
	case uint64:
		printuint(x)
	case uintptr:
		printuint(uint64(x))
	case float32:
		printfloat32(x)
	case float64:
		printfloat64(x)
	case complex64:
		printcomplex64(x)
	case complex128:
		printcomplex128(x)
	case string:
		printindented(x)
	default:
		printanycustomtype(v)
	}
}

// printanycustomtype prints values of defined types with a basic underlying
// type as T(v), and anything else as (T) address.
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
	case k == kindComplex64:
		printstring(ts)
		printcomplex64(*(*complex64)(p))
	case k == kindComplex128:
		printstring(ts)
		printcomplex128(*(*complex128)(p))
	case k == kindBool || k >= kindInt && k <= kindFloat64:
		printstring(ts)
		printstring("(")
		switch {
		case k == kindBool:
			printbool(*(*bool)(p))
		case k <= kindInt64:
			printint(loadInt(p, e.typ.size))
		case k <= kindUintptr:
			printuint(loadUint(p, e.typ.size))
		case k == kindFloat32:
			printfloat32(*(*float32)(p))
		default:
			printfloat64(*(*float64)(p))
		}
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
	// deferfp is the frame address of the code that calls deferred
	// functions for this panic. recover compares against it.
	deferfp uintptr
	// wrapped reports whether the deferred call being run is a compiler
	// generated wrapper around a direct call (defer f(args)).
	wrapped   bool
	recovered bool
	aborted   bool
	goexit    bool
	// repanicked marks a panic whose value was recovered and panicked
	// again unchanged.
	repanicked bool
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
	printpanicval(p.arg)
	if p.recovered && p.repanicked {
		printstring(" [recovered, repanicked]")
	} else if p.recovered {
		printstring(" [recovered]")
	}
	printstring("\n")
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
			p.arg = s
		}
		p = p.link
	}
}

// fatalpanic prints the panic chain and the goroutine header and exits
// with status 2.
func fatalpanic(p *_panic, goid int64) {
	preprintpanics(p)
	printlock()
	printpanics(p)
	printstring("\ngoroutine ")
	printint(goid)
	printstring(" [running]:\n")
	printunlock()
	exit(2)
}
