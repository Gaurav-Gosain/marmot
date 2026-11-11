// Host implementations of the runtime primitives (see
// lib/src/runtime/prim_marmot.go for the list). They let the pure runtime
// files run as an ordinary Go package under the host toolchain, with real
// threads, so `go test -race` exercises channels and select.

package runtime

import (
	"bytes"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

type mutex struct{ m sync.Mutex }

func lock(l *mutex)   { l.m.Lock() }
func unlock(l *mutex) { l.m.Unlock() }

var zerobase uintptr

// mallocgc allocates host memory. Memory that holds pointers is allocated
// as []unsafe.Pointer so the host collector scans it.
func mallocgc(size uintptr, typ *_type) unsafe.Pointer {
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	words := (size + 7) / 8
	if typ == nil || typ.ptrdata == 0 {
		b := make([]uint64, words)
		return unsafe.Pointer(&b[0])
	}
	b := make([]unsafe.Pointer, words)
	return unsafe.Pointer(&b[0])
}

func mallocConservative(size uintptr) unsafe.Pointer {
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	b := make([]unsafe.Pointer, (size+7)/8)
	return unsafe.Pointer(&b[0])
}

func mallocPrecise(size uintptr, bits *byte, elemsize uintptr) unsafe.Pointer {
	return mallocConservative(size)
}

func memmove(dst, src unsafe.Pointer, n uintptr) {
	if n == 0 {
		return
	}
	copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
}

func memclrNoHeapPointers(p unsafe.Pointer, n uintptr) {
	if n == 0 {
		return
	}
	clear(unsafe.Slice((*byte)(p), n))
}

func memequal(a, b unsafe.Pointer, n uintptr) bool {
	if a == b || n == 0 {
		return true
	}
	return bytes.Equal(unsafe.Slice((*byte)(a), n), unsafe.Slice((*byte)(b), n))
}

func memcmp(a, b unsafe.Pointer, n uintptr) int {
	if n == 0 {
		return 0
	}
	return bytes.Compare(unsafe.Slice((*byte)(a), n), unsafe.Slice((*byte)(b), n))
}

// Output written by the print functions, for tests to inspect.
var (
	outMu sync.Mutex
	out   bytes.Buffer
)

func writeErr(p unsafe.Pointer, n uintptr) {
	outMu.Lock()
	out.Write(unsafe.Slice((*byte)(p), n))
	outMu.Unlock()
}

// takeOutput returns and clears the captured output.
func takeOutput() string {
	outMu.Lock()
	defer outMu.Unlock()
	s := out.String()
	out.Reset()
	return s
}

// hostG is the host side of a goroutine: a wake-up channel.
type hostG struct {
	wake chan struct{}
}

var (
	gsMu sync.Mutex
	gs   = map[int64]*g{}
	goid atomic.Int64
)

// curGoid parses the goroutine id from the stack header.
func curGoid() int64 {
	var buf [64]byte
	n := goruntime.Stack(buf[:], false)
	b := buf[:n]
	b = b[len("goroutine "):]
	var id int64
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + int64(c-'0')
	}
	return id
}

func getg() *g {
	id := curGoid()
	gsMu.Lock()
	gp := gs[id]
	if gp == nil {
		gp = &g{}
		gp.handle = unsafe.Pointer(&hostG{wake: make(chan struct{}, 1)})
		gp.goid = goid.Add(1)
		gp.rand = uint64(id)*0x9e3779b97f4a7c15 + 1
		gs[id] = gp
	}
	gsMu.Unlock()
	return gp
}

// forgetg drops the g of the current goroutine; tests call it before a
// goroutine ends so the table does not grow without bound.
func forgetg() {
	id := curGoid()
	gsMu.Lock()
	delete(gs, id)
	gsMu.Unlock()
}

func gopark(unlockf func(unsafe.Pointer), lock unsafe.Pointer) {
	hg := (*hostG)(getg().handle)
	if unlockf != nil {
		unlockf(lock)
	}
	<-hg.wake
}

func goparkunlock(l *mutex) { gopark(parkunlock, unsafe.Pointer(l)) }

func goparkselect(gp *g) { gopark(selparkcommit, unsafe.Pointer(gp)) }

func goparkForever() { select {} }

func goready(gp *g) {
	(*hostG)(gp.handle).wake <- struct{}{}
}

// throwError is the panic value used by throw on the host.
type throwError string

func throw(s string) {
	panic(throwError("fatal error: " + s))
}

// exitError is the panic value used by exit on the host.
type exitError int32

func exit(code int32) {
	panic(exitError(code))
}

func nanotime() int64 { return 0 }

func atomicLoad32(p *uint32) uint32                    { return atomic.LoadUint32(p) }
func atomicStore32(p *uint32, v uint32)                { atomic.StoreUint32(p, v) }
func atomicCas32(p *uint32, old, new uint32) bool      { return atomic.CompareAndSwapUint32(p, old, new) }
func atomicLoadp(p *unsafe.Pointer) unsafe.Pointer     { return atomic.LoadPointer(p) }
func atomicStorep(p *unsafe.Pointer, v unsafe.Pointer) { atomic.StorePointer(p, v) }
func atomicLoad64(p *uint64) uint64                    { return atomic.LoadUint64(p) }
func atomicStore64(p *uint64, v uint64)                { atomic.StoreUint64(p, v) }
func atomicCas64(p *uint64, old, new uint64) bool      { return atomic.CompareAndSwapUint64(p, old, new) }
func atomicOr64(p *uint64, v uint64)                   { atomic.OrUint64(p, v) }
func atomicXadd32(p *uint32, d int32) uint32           { return atomic.AddUint32(p, uint32(d)) }
func atomicXadd64(p *uint64, d int64) uint64           { return atomic.AddUint64(p, uint64(d)) }

// _defer is defined in panic.go, which is marmot only.
type _defer struct{}

func hashBytes(p unsafe.Pointer, n uintptr, seed uintptr) uintptr {
	return memhash(p, seed, n)
}

func procyield(n int32) { goruntime.Gosched() }
