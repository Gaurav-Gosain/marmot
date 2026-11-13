package runtime

import "unsafe"

// cgo's pointer passing check (gc's GODEBUG=cgocheck=1, on by default,
// off with cgocheck=0): Go code may pass C a pointer to Go memory only if
// that memory holds no Go pointers that are not pinned. The code cgo
// generates calls cgoCheckPointer for each argument whose type may hold
// pointers. Go memory here is the heap (rt_heap_object); pointers into
// package variables are not checked, and neither is memory passed as an
// unsafe.Pointer whose type is no longer known.

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

// cgoCheckPointer checks the argument ptr. arg is true when ptr is &x and
// all of x counts (cgo writes "0 == 0"); the check is the same here.
func cgoCheckPointer(ptr any, arg any) {
	if !cgocheckOn() {
		return
	}
	e := efaceOf(&ptr)
	t := e.typ
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
			panic(errorString("argument of cgo function has Go pointer to unpinned Go pointer"))
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
