package runtime

import "unsafe"

// cgo's pointer passing check (gc's GODEBUG=cgocheck=1, on by default,
// off with cgocheck=0): Go code may pass C a pointer to Go memory only if
// that memory holds no Go pointers that are not pinned. The code cgo
// generates calls cgoCheckPointer for each argument whose type may hold
// pointers. Go memory here is the heap (rt_heap_object); pointers into
// package variables are not checked. For an unsafe.Pointer argument the
// heap's pointer bits of the object stand in for its type.

var cgocheckState int32 // 0: unknown, 1: on, 2: off

func cgocheckOn() bool {
	if cgocheckState == 0 {
		cgocheckState = 1
		for _, e := range goenvs() {
			if len(e) > 8 && e[:8] == "GODEBUG=" && godebugHas(e[8:], "cgocheck=0") {
				cgocheckState = 2
			}
		}
	}
	return cgocheckState == 1
}

func godebugHas(list, kv string) bool {
	for len(list) > 0 {
		i := 0
		for i < len(list) && list[i] != ',' {
			i++
		}
		if list[:i] == kv {
			return true
		}
		if i == len(list) {
			break
		}
		list = list[i+1:]
	}
	return false
}

// cgoPointerError is the panic of a failed check. Its message is gc's; the
// panic report prints the detail under it (which call, which argument,
// where in the memory, what to do).
type cgoPointerError struct{ detail string }

func (e cgoPointerError) Error() string {
	return "runtime error: argument of cgo function has Go pointer to unpinned Go pointer"
}

func (cgoPointerError) RuntimeError() {}

// cgoCheckPointerFor is cgoCheckPointer for argument n of the C function
// fn (marmot's cgo output passes them).
func cgoCheckPointerFor(fn string, n int, ptr any, arg any) {
	if !cgocheckOn() {
		return
	}
	cgoCall, cgoArg = fn, n
	cgoCheckPointer(ptr, arg)
	cgoCall = ""
}

// The call being checked (one goroutine panics at a time in practice; the
// detail is best effort).
var (
	cgoCall string
	cgoArg  int
)

func cgoFail(t *_type, off uintptr) {
	if cgoCall == "" {
		panic(errorString("argument of cgo function has Go pointer to unpinned Go pointer"))
	}
	what := "memory"
	if t != nil {
		what = "a " + cgoTypeName(t.str)
	}
	d := "Go side: argument " + itoa(int64(cgoArg+1)) + " of C." + cgoCall + " points to " + what +
		" holding a Go pointer at offset " + itoa(int64(off)) + "\n" +
		"C side: C." + cgoCall + " may keep or follow that pointer, where the garbage collector cannot see it\n" +
		"fix: pin the pointed-to value with runtime.Pinner for the call, or keep the data in C memory (C.malloc)"
	cgoCall = ""
	panic(cgoPointerError{d})
}

// cgoTypeName shows cgo's generated type names as Go code spells them:
// main._Ctype_holder is C.holder; an anonymous struct is a C struct.
func cgoTypeName(s string) string {
	for i := 0; i+7 <= len(s); i++ {
		if s[i:i+7] != "_Ctype_" {
			continue
		}
		rest := s[i+7:]
		if len(rest) >= 9 && rest[:9] == "struct___" {
			return "C struct"
		}
		return "C." + rest
	}
	return s
}

// cgoCheckPointer checks the argument ptr. arg is true when ptr is &x and
// all of x counts (cgo writes "0 == 0"); the check is the same here.
func cgoCheckPointer(ptr any, arg any) {
	if !cgocheckOn() {
		return
	}
	e := efaceOf(&ptr)
	t := e.typ
	if t != nil && t.kind == kindUnsafePointer {
		cgoCheckUnknownPointer(e.data)
		return
	}
	if t == nil || t.kind != kindPointer {
		return
	}
	p := e.data
	if t.tflag&tflagDirectIface == 0 {
		p = *(*unsafe.Pointer)(p)
	}
	if p == nil || rt_heap_object(p) == 0 {
		return
	}
	cgoCheckValue((*ptrtype)(unsafe.Pointer(t)).elem, p)
}

// cgoCheckUnknownPointer checks memory passed as an unsafe.Pointer, whose
// type is known only to the heap: the whole object p points into.
// Objects scanned conservatively (no pointer bits) are not checked.
func cgoCheckUnknownPointer(p unsafe.Pointer) {
	if p == nil {
		return
	}
	var base unsafe.Pointer
	var size uintptr
	if rt_heap_object_span(p, &base, &size) != rtHeapObjPrecise {
		return
	}
	for off := uintptr(0); off+ptrSize <= size; off += ptrSize {
		if rt_heap_ptr_word(base, off) == 0 {
			continue
		}
		v := *(*unsafe.Pointer)(unsafe.Add(base, off))
		if v != nil && rt_heap_object(v) != 0 && !isPinned(v) {
			cgoFail(nil, off)
		}
	}
}

// rt_heap_object_span kinds (rt.h).
const rtHeapObjPrecise = 2

//go:linkname rt_heap_object_span rt_heap_object_span
//go:noescape
func rt_heap_object_span(p unsafe.Pointer, base *unsafe.Pointer, size *uintptr) int32

//go:linkname rt_heap_ptr_word rt_heap_ptr_word
func rt_heap_ptr_word(base unsafe.Pointer, off uintptr) int32

// cgoCheckValue panics if the value of type t at p holds a pointer to
// the Go heap that is not pinned.
func cgoCheckValue(t *_type, p unsafe.Pointer) {
	if t.ptrdata == 0 || t.gcdata == nil {
		return
	}
	for i := uintptr(0); i < t.ptrdata/ptrSize; i++ {
		if *(*byte)(unsafe.Add(unsafe.Pointer(t.gcdata), i/8))>>(i%8)&1 == 0 {
			continue
		}
		v := *(*unsafe.Pointer)(unsafe.Add(p, i*ptrSize))
		if v != nil && rt_heap_object(v) != 0 && !isPinned(v) {
			cgoFail(t, i*ptrSize)
		}
	}
}

// Pinned pointers, for the check (Pinner).
var (
	pinnerLock mutex
	pins    map[unsafe.Pointer]int
)

func pinAdd(p unsafe.Pointer, d int) {
	lock(&pinnerLock)
	if pins == nil {
		pins = map[unsafe.Pointer]int{}
	}
	if pins[p] += d; pins[p] <= 0 {
		delete(pins, p)
	}
	unlock(&pinnerLock)
}

func isPinned(p unsafe.Pointer) bool {
	lock(&pinnerLock)
	n := pins[p]
	unlock(&pinnerLock)
	return n > 0
}
