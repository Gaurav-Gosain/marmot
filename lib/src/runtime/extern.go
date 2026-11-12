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

// Stack traces. Callers walks the frame record chain; the backend's pc
// table (pcln.go) maps the pcs to functions, files and lines, and (table
// version 3) to the frames of calls that LLVM inlined.

// The pc table of the binary, loaded on first use.
var (
	pclnTab   pclnTable
	pclnState uint32 // 0: not loaded yet, 1: loaded, 2: the binary has none
)

func pclntab() *pclnTable {
	switch atomicLoad32(&pclnState) {
	case 1:
		return &pclnTab
	case 2:
		return nil
	}
	var data unsafe.Pointer
	var n int
	var base uintptr
	if rt_pcln(&data, &n, &base) != 0 {
		// Racing loaders store the same values.
		if pclnTab.init(data, uintptr(n), base) {
			atomicStore32(&pclnState, 1)
			return &pclnTab
		}
	}
	atomicStore32(&pclnState, 2)
	return nil
}

// pcLookup describes the function holding pc.
func pcLookup(pc uintptr) (pcInfo, bool) {
	t := pclntab()
	if t == nil {
		return pcInfo{}, false
	}
	return t.lookup(pc)
}

// maxInlineDepth bounds the logical frames taken for one pc.
const maxInlineDepth = 32

// pcFrames stores the logical frames at pc (inlined calls first) in out.
func pcFrames(pc uintptr, out []pcInfo) int {
	t := pclntab()
	if t == nil {
		return 0
	}
	return t.frames(pc, out)
}

// Caller reports file and line number information about function
// invocations on the calling goroutine's stack. skip 0 is the caller of
// Caller.
//
//go:noinline
func Caller(skip int) (pc uintptr, file string, line int, ok bool) {
	if skip < 0 {
		return 0, "", 0, false
	}
	// skip counts logical frames: walk physical frames and expand the
	// inlined calls of each.
	var rpc [64]uintptr
	n := callers(getcallerfp(), 0, rpc[:])
	var fr [maxInlineDepth]pcInfo
	for _, p := range rpc[:n] {
		k := pcFrames(p-1, fr[:])
		if k == 0 {
			if skip == 0 {
				return p, "", 0, false
			}
			skip--
			continue
		}
		if skip < k {
			f := fr[skip]
			return p, f.filePrefix + f.file, f.line, true
		}
		skip -= k
	}
	return 0, "", 0, false
}

// Callers fills pc with the return program counters of function
// invocations on the calling goroutine's stack, found by walking the
// frame record chain. skip 0 and 1 both start at the caller of Callers
// (marmot has no pc for Callers itself).
//
//go:noinline
func Callers(skip int, pc []uintptr) int {
	if skip > 0 {
		skip--
	}
	return callers(getcallerfp(), skip, pc)
}

// callers walks the frame records from fp, skips skip frames and stores
// the return addresses in pc. The walk stops at a zero or non-increasing
// frame pointer (the base of a goroutine or thread stack).
func callers(fp uintptr, skip int, pc []uintptr) int {
	n := 0
	for fp != 0 && n < len(pc) {
		next := *(*uintptr)(unsafe.Pointer(fp))
		ret := *(*uintptr)(unsafe.Pointer(fp + goarch.PtrSize))
		if ret == 0 {
			break
		}
		if skip > 0 {
			skip--
		} else {
			pc[n] = ret
			n++
		}
		if next <= fp || next-fp > 1<<24 {
			break
		}
		fp = next
	}
	return n
}

// Func represents a function in the running binary.
type Func struct {
	opaque struct{}
	entry  uintptr
	name   string
}

// FuncForPC returns a *Func describing the function at pc, or nil. If pc
// is in code inlined from other functions, it describes the innermost
// one, with the entry of the outermost (as gc). pc is often a return
// address (from Caller or Callers): when pc itself is outside inlined
// code but pc-1 (the call) is inside, the call decides.
func FuncForPC(pc uintptr) *Func {
	var fr [maxInlineDepth]pcInfo
	n := pcFrames(pc, fr[:])
	if n == 0 {
		return nil
	}
	if n == 1 && pc > fr[0].entry {
		var fr1 [maxInlineDepth]pcInfo
		if m := pcFrames(pc-1, fr1[:]); m > 1 && fr1[m-1].entry == fr[0].entry {
			fr, n = fr1, m
		}
	}
	inner := fr[0]
	return &Func{entry: fr[n-1].entry, name: inner.namePrefix + inner.name}
}

// Name returns the name of the function.
func (f *Func) Name() string {
	if f == nil {
		return ""
	}
	return f.name
}

// Entry returns the entry address of the function.
func (f *Func) Entry() uintptr {
	if f == nil {
		return 0
	}
	return f.entry
}

// FileLine returns the file name and line number of pc.
func (f *Func) FileLine(pc uintptr) (file string, line int) {
	info, ok := pcLookup(pc)
	if !ok {
		return "?", 0
	}
	return info.filePrefix + info.file, info.line
}

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
	pending []Frame // logical frames of the current pc not returned yet
}

// CallersFrames takes a slice of PCs returned by Callers.
func CallersFrames(callers []uintptr) *Frames {
	return &Frames{callers: callers}
}

// Next returns a Frame and whether there are more frames.
func (ci *Frames) Next() (frame Frame, more bool) {
	if len(ci.pending) == 0 {
		if len(ci.callers) == 0 {
			return Frame{}, false
		}
		pc := ci.callers[0]
		ci.callers = ci.callers[1:]
		var fr [maxInlineDepth]pcInfo
		n := pcFrames(pc-1, fr[:])
		if n == 0 {
			ci.pending = append(ci.pending, Frame{PC: pc})
		}
		for i, info := range fr[:n] {
			f := Frame{
				PC:       pc,
				Function: info.namePrefix + info.name,
				File:     info.filePrefix + info.file,
				Line:     info.line,
			}
			if i == n-1 {
				// The physical function; inlined frames have no entry
				// (as in gc).
				f.Entry = info.entry
				f.Func = &Func{entry: info.entry, name: f.Function}
			}
			ci.pending = append(ci.pending, f)
		}
	}
	frame = ci.pending[0]
	ci.pending = ci.pending[1:]
	return frame, len(ci.pending) > 0 || len(ci.callers) > 0
}

// Stack formats a stack trace of the calling goroutine into buf and
// returns the number of bytes written. all is ignored: marmot prints only
// the calling goroutine.
//
//go:noinline
func Stack(buf []byte, all bool) int {
	n := copy(buf, "goroutine "+itoa(getg().goid)+" [running]:\n")
	var pcs [64]uintptr
	k := callers(getcallerfp(), 0, pcs[:])
	var fr [maxInlineDepth]pcInfo
	for _, pc := range pcs[:k] {
		for _, info := range fr[:pcFrames(pc-1, fr[:])] {
			if info.line == 0 && info.file == "" {
				continue
			}
			n += copy(buf[n:], info.namePrefix+info.name+"(...)\n\t"+info.filePrefix+info.file+":"+itoa(int64(info.line))+"\n")
		}
	}
	return n
}

// printTraceback prints the frames above fp, as in a fatal panic, without
// allocating. Leading runtime frames (the panic machinery) are skipped.
func printTraceback(fp uintptr) {
	var pcs [100]uintptr
	k := callers(fp, 0, pcs[:])
	leading := true
	var fr [maxInlineDepth]pcInfo
	for _, pc := range pcs[:k] {
		nf := pcFrames(pc-1, fr[:])
		for _, info := range fr[:nf] {
			if info.line == 0 && info.file == "" || leading && info.namePrefix == "runtime." {
				// C frames of the runtime core and the panic machinery.
				continue
			}
			leading = false
			printFrame(info, pc)
		}
	}
}

// printFrame prints one traceback frame; pc is the return address of the
// physical frame.
func printFrame(info pcInfo, pc uintptr) {
	printstring(info.namePrefix)
	printstring(info.name)
	printstring("(...)\n\t")
	printstring(info.filePrefix)
	printstring(info.file)
	printstring(":")
	printint(int64(info.line))
	printstring(" +")
	printhex(uint64(pc - info.entry))
	printstring("\n")
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

// Finalizers and cleanups.
//
// Each object with a finalizer or cleanups has one entry in finTable and
// one rt_gc_set_finalizer registration, whose callback runs them. Keys are
// stored inverted (^address): the table's memory is scanned
// conservatively, and an inverted address does not keep the object alive.

type finEntry struct {
	fn       any    // finalizer func value, nil if none
	argIface bool   // the finalizer takes an interface parameter
	typ      *_type // type of the object pointer
	cleanups []cleanupEntry
}

type cleanupEntry struct {
	id uint64
	fn func()
}

var (
	finLock       mutex
	finTable      map[uintptr]*finEntry
	cleanupNextID uint64
)

// finEntryFor returns the entry for obj, creating it and registering the
// collector callback on first use. finLock must be held.
func finEntryFor(obj unsafe.Pointer) *finEntry {
	if finTable == nil {
		finTable = make(map[uintptr]*finEntry)
	}
	key := ^uintptr(obj)
	e := finTable[key]
	if e == nil {
		e = &finEntry{}
		finTable[key] = e
		fin := runFinalizer
		rt_gc_set_finalizer(obj, funcPC(unsafe.Pointer(&fin)))
	}
	return e
}

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
	if e.data == nil {
		throw("nil elem type!")
	}
	f := efaceOf(&finalizer)
	argIface := false
	if f.typ != nil {
		if f.typ.kind != kindFunc {
			throw("runtime.SetFinalizer: second argument is " + f.typ.str + ", not a function")
		}
		ft := (*functype)(unsafe.Pointer(f.typ))
		argIface = ft.inCount == 1 && ft.param(0).kind == kindInterface
	}
	lock(&finLock)
	if f.typ == nil {
		if ent := finTable[^uintptr(e.data)]; ent != nil {
			ent.fn = nil
		}
		unlock(&finLock)
		return
	}
	ent := finEntryFor(e.data)
	ent.fn = finalizer
	ent.argIface = argIface
	ent.typ = e.typ
	unlock(&finLock)
}

// finRunning is the number of runFinalizer calls in progress and finRuns
// the number started, for blockUntilEmptyFinalizerQueue.
var finRunning, finRuns uint64

// blockUntilEmptyFinalizerQueue waits until no finalizer or cleanup is
// running and none has started for a quiet period, or until timeout
// nanoseconds pass, and reports whether the queue drained. The C core's
// queue is not visible, so a finalizer queued but not started within the
// quiet period is missed. Used by tests (sync, unique, runtime).
func blockUntilEmptyFinalizerQueue(timeout int64) bool {
	const quiet = 2e6 // ns
	start := nanotime()
	runs := atomicLoad64(&finRuns)
	since := start
	for {
		now := nanotime()
		if r := atomicLoad64(&finRuns); r != runs || atomicLoad64(&finRunning) != 0 {
			runs, since = r, now
		} else if now-since >= quiet {
			return true
		}
		if now-start >= timeout {
			return false
		}
		rt_sleep(100e3)
	}
}

//go:linkname unique_runtime_blockUntilEmptyCleanupQueue unique.runtime_blockUntilEmptyCleanupQueue
func unique_runtime_blockUntilEmptyCleanupQueue(timeout int64) bool {
	return blockUntilEmptyFinalizerQueue(timeout)
}

//go:linkname sync_test_runtime_blockUntilEmptyCleanupQueue sync_test.runtime_blockUntilEmptyCleanupQueue
func sync_test_runtime_blockUntilEmptyCleanupQueue(timeout int64) bool {
	return blockUntilEmptyFinalizerQueue(timeout)
}

// haveHighResSleep reports that time.Sleep has about 1 ms resolution or
// better (read by the time package tests).
var haveHighResSleep = true

// runFinalizer is called by the collector, on a runtime goroutine, for an
// object that became unreachable.
func runFinalizer(obj unsafe.Pointer) {
	atomicXadd64(&finRuns, 1)
	atomicXadd64(&finRunning, 1)
	defer atomicXadd64(&finRunning, -1)
	lock(&finLock)
	key := ^uintptr(obj)
	ent := finTable[key]
	delete(finTable, key)
	unlock(&finLock)
	if ent == nil {
		return
	}
	for _, c := range ent.cleanups {
		c.fn()
	}
	if ent.fn == nil {
		return
	}
	// The finalizer is func(*T) or func(any); results are ignored.
	fv := efaceOf(&ent.fn).data
	if ent.argIface {
		var arg any
		ae := efaceOf(&arg)
		ae.typ = ent.typ
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
	id  uint64
	key uintptr // ^address of the object
}

// AddCleanup attaches a cleanup function to ptr: some time after ptr
// becomes unreachable, the runtime calls cleanup(arg) on a separate
// goroutine. cleanup and arg must not reference ptr, or it never becomes
// unreachable.
func AddCleanup[T, S any](ptr *T, cleanup func(S), arg S) Cleanup {
	if ptr == nil {
		throw("runtime.AddCleanup: ptr is nil")
	}
	p := unsafe.Pointer(ptr)
	fn := func() { cleanup(arg) }
	lock(&finLock)
	cleanupNextID++
	id := cleanupNextID
	ent := finEntryFor(p)
	ent.cleanups = append(ent.cleanups, cleanupEntry{id: id, fn: fn})
	unlock(&finLock)
	return Cleanup{id: id, key: ^uintptr(p)}
}

// Stop cancels the cleanup call.
func (c Cleanup) Stop() {
	if c.id == 0 {
		return
	}
	lock(&finLock)
	if ent := finTable[c.key]; ent != nil {
		for i, ce := range ent.cleanups {
			if ce.id == c.id {
				ent.cleanups = append(ent.cleanups[:i], ent.cleanups[i+1:]...)
				break
			}
		}
	}
	unlock(&finLock)
}

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
