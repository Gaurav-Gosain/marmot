// The public runtime API that ordinary code uses. Compiled only by marmot.

package runtime

import (
	"internal/goarch"
	"internal/goos"
	"unsafe"
)

// GOOS is the running program's operating system target.
const GOOS string = goos.GOOS

// GOARCH is the running program's architecture target.
const GOARCH string = goarch.GOARCH

// Compiler is the name of the compiler toolchain that built the binary.
const Compiler = "gc"

// buildVersion is the Go tree's version string at build time.
var buildVersion = "go1.27"

// Version returns the Go tree's version string.
func Version() string { return buildVersion }

// GOROOT returns the root of the Go tree, from $GOROOT if set.
func GOROOT() string {
	s := gogetenv("GOROOT")
	if s != "" {
		return s
	}
	return ""
}

var (
	argsCache []string
	envsCache []string
)

// cstring converts a NUL terminated C string.
func cstring(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	s, b := rawstring(n)
	memmove(unsafe.Pointer(unsafe.SliceData(b)), p, uintptr(n))
	return s
}

func goargs() []string {
	if argsCache == nil {
		n := int(rt_argc())
		argv := rt_argv()
		a := make([]string, n)
		for i := 0; i < n; i++ {
			a[i] = cstring(*(*unsafe.Pointer)(unsafe.Add(argv, uintptr(i)*ptrSize)))
		}
		argsCache = a
	}
	return argsCache
}

func goenvs() []string {
	if envsCache == nil {
		envp := rt_envp()
		n := 0
		for *(*unsafe.Pointer)(unsafe.Add(envp, uintptr(n)*ptrSize)) != nil {
			n++
		}
		e := make([]string, n)
		for i := 0; i < n; i++ {
			e[i] = cstring(*(*unsafe.Pointer)(unsafe.Add(envp, uintptr(i)*ptrSize)))
		}
		envsCache = e
	}
	return envsCache
}

func gogetenv(key string) string {
	for _, s := range goenvs() {
		if len(s) > len(key) && s[len(key)] == '=' && s[:len(key)] == key {
			return s[len(key)+1:]
		}
	}
	return ""
}

// LockOSThread wires the calling goroutine to its current OS thread.
// marmot does not move goroutines between threads while they run user
// code, so this is a no-op.
func LockOSThread() {}

// UnlockOSThread undoes an earlier call to LockOSThread.
func UnlockOSThread() {}

// Breakpoint executes a breakpoint trap.
func Breakpoint() { throw("runtime.Breakpoint") }

// NumCgoCall returns the number of cgo calls made by the current process.
func NumCgoCall() int64 { return 0 }

// SetMutexProfileFraction and SetBlockProfileRate control profiling, which
// marmot does not implement.
func SetMutexProfileFraction(rate int) int { return 0 }
func SetBlockProfileRate(rate int)         {}

// Stack traces. marmot keeps no unwind tables, so callers are unknown.

// Caller reports file and line number information about function
// invocations on the calling goroutine's stack. marmot has no unwind
// information and always reports ok == false.
func Caller(skip int) (pc uintptr, file string, line int, ok bool) {
	return 0, "", 0, false
}

// Callers fills pc with the return program counters of function
// invocations; marmot records none.
func Callers(skip int, pc []uintptr) int { return 0 }

// Func represents a function in the running binary.
type Func struct {
	opaque struct{}
}

// FuncForPC returns a *Func describing the function at pc, or nil. marmot
// has no function table and returns nil.
func FuncForPC(pc uintptr) *Func { return nil }

// Name returns the name of the function.
func (f *Func) Name() string { return "" }

// Entry returns the entry address of the function.
func (f *Func) Entry() uintptr { return 0 }

// FileLine returns the file name and line number of pc.
func (f *Func) FileLine(pc uintptr) (file string, line int) { return "", 0 }

// Frame is the information returned by Frames for each call frame.
type Frame struct {
	PC        uintptr
	Func      *Func
	Function  string
	File      string
	Line      int
	startLine int
	Entry     uintptr
}

// Frames may be used to get function/file/line information for a slice of
// PC values returned by Callers.
type Frames struct {
	callers []uintptr
}

// CallersFrames takes a slice of PCs returned by Callers.
func CallersFrames(callers []uintptr) *Frames {
	return &Frames{callers: callers}
}

// Next returns a Frame and whether there are more frames.
func (ci *Frames) Next() (frame Frame, more bool) {
	if len(ci.callers) == 0 {
		return Frame{}, false
	}
	frame.PC = ci.callers[0]
	ci.callers = ci.callers[1:]
	return frame, len(ci.callers) > 0
}

// Stack formats a stack trace of the calling goroutine into buf and
// returns the number of bytes written. marmot writes only the header.
func Stack(buf []byte, all bool) int {
	s := "goroutine " + itoa(getg().goid) + " [running]:\n"
	return copy(buf, s)
}

// MemStats records statistics about the memory allocator.
type MemStats struct {
	Alloc         uint64
	TotalAlloc    uint64
	Sys           uint64
	Lookups       uint64
	Mallocs       uint64
	Frees         uint64
	HeapAlloc     uint64
	HeapSys       uint64
	HeapIdle      uint64
	HeapInuse     uint64
	HeapReleased  uint64
	HeapObjects   uint64
	StackInuse    uint64
	StackSys      uint64
	MSpanInuse    uint64
	MSpanSys      uint64
	MCacheInuse   uint64
	MCacheSys     uint64
	BuckHashSys   uint64
	GCSys         uint64
	OtherSys      uint64
	NextGC        uint64
	LastGC        uint64
	PauseTotalNs  uint64
	PauseNs       [256]uint64
	PauseEnd      [256]uint64
	NumGC         uint32
	NumForcedGC   uint32
	GCCPUFraction float64
	EnableGC      bool
	DebugGC       bool
	BySize        [61]struct {
		Size    uint32
		Mallocs uint64
		Frees   uint64
	}
}

// ReadMemStats populates m with memory allocator statistics.
func ReadMemStats(m *MemStats) {
	var s rtMemstats
	rt_gc_stats(&s)
	*m = MemStats{}
	m.Alloc = s.heapAlloc
	m.HeapAlloc = s.heapAlloc
	m.TotalAlloc = s.totalAlloc
	m.Sys = s.heapSys
	m.HeapSys = s.heapSys
	m.HeapInuse = s.heapAlloc
	m.Mallocs = s.mallocs
	m.Frees = s.frees
	m.HeapObjects = s.mallocs - s.frees
	m.NumGC = uint32(s.numGC)
	m.PauseTotalNs = s.pauseTotalNs
	m.EnableGC = true
}

// Finalizers.

const finTabMin = 64

// finTab maps objects to finalizers. Keys are stored as uintptr in noscan
// memory so that the table does not keep objects alive.
var (
	finLock  mutex
	finKeys  []uintptr
	finFuncs []any // the finalizer func value
	finArgIf []bool
	finTypes []*_type
)

// SetFinalizer sets the finalizer associated with obj to finalizer, which
// is called once in a separate goroutine when obj becomes unreachable.
// A nil finalizer clears it.
func SetFinalizer(obj any, finalizer any) {
	e := efaceOf(&obj)
	if e.typ == nil {
		throw("runtime.SetFinalizer: first argument is nil")
	}
	if e.typ.kind != kindPointer {
		throw("runtime.SetFinalizer: first argument is " + e.typ.str + ", not pointer")
	}
	p := uintptr(e.data)
	if p == 0 {
		throw("nil elem type!")
	}
	f := efaceOf(&finalizer)
	lock(&finLock)
	idx := -1
	for i, k := range finKeys {
		if k == p {
			idx = i
			break
		}
	}
	if f.typ == nil {
		if idx >= 0 {
			finFuncs[idx] = nil
			finKeys[idx] = 0
		}
		unlock(&finLock)
		return
	}
	if f.typ.kind != kindFunc {
		unlock(&finLock)
		throw("runtime.SetFinalizer: second argument is " + f.typ.str + ", not a function")
	}
	ft := (*functype)(unsafe.Pointer(f.typ))
	argIsIface := ft.inCount == 1 && ft.param(0).kind == kindInterface
	if idx < 0 {
		for i, k := range finKeys {
			if k == 0 {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		finKeys = append(finKeys, p)
		finFuncs = append(finFuncs, finalizer)
		finArgIf = append(finArgIf, argIsIface)
		finTypes = append(finTypes, e.typ)
	} else {
		finKeys[idx] = p
		finFuncs[idx] = finalizer
		finArgIf[idx] = argIsIface
		finTypes[idx] = e.typ
	}
	unlock(&finLock)
	fin := runFinalizer
	rt_gc_set_finalizer(e.data, funcPC(unsafe.Pointer(&fin)))
}

// runFinalizer is called by the collector for an object with a finalizer.
func runFinalizer(obj unsafe.Pointer) {
	p := uintptr(obj)
	lock(&finLock)
	var fn any
	var argIf bool
	var typ *_type
	for i, k := range finKeys {
		if k == p {
			fn = finFuncs[i]
			argIf = finArgIf[i]
			typ = finTypes[i]
			finKeys[i] = 0
			finFuncs[i] = nil
			finTypes[i] = nil
			break
		}
	}
	unlock(&finLock)
	if fn == nil {
		return
	}
	// fn is func(*T) or func(any) (results are ignored).
	fv := efaceOf(&fn).data
	if argIf {
		var arg any
		ae := efaceOf(&arg)
		ae.typ = typ
		ae.data = obj
		f := *(*func(any))(unsafe.Pointer(&fv))
		f(arg)
		return
	}
	f := *(*func(unsafe.Pointer))(unsafe.Pointer(&fv))
	f(obj)
}

// Cleanup is a handle to a cleanup call for a specific object.
type Cleanup struct {
	id uint64
}

// AddCleanup attaches a cleanup function to ptr. marmot runs cleanups only
// when the collector supports it; the call is accepted and may never run.
func AddCleanup[T, S any](ptr *T, cleanup func(S), arg S) Cleanup {
	return Cleanup{}
}

// Stop cancels the cleanup call.
func (c Cleanup) Stop() {}

// A Pinner is a set of Go objects each pinned to a fixed location in
// memory. marmot's collector never moves objects, so pinning is a no-op.
type Pinner struct {
	refs []unsafe.Pointer
}

// Pin pins a Go object.
func (p *Pinner) Pin(pointer any) {
	p.refs = append(p.refs, efaceOf(&pointer).data)
}

// Unpin unpins all pinned objects of the Pinner.
func (p *Pinner) Unpin() {
	p.refs = nil
}
