// Execution tracing (runtime.StartTrace, ReadTrace, StopTrace) on the trace
// writer of the runtime core (runtime/core/src/tracev2.c), in the format
// runtime/trace and golang.org/x/exp/trace read (version Go126).
//
// The core writes the event batches and a table of stacks as return
// addresses. This file switches generations (once a period, and when
// runtime/trace asks), turns each closed generation's stack table into the
// strings and stacks batches, adds the sync batch and queues it all for
// ReadTrace: one batch per chunk, then a one-byte EvEndOfGeneration chunk.
//
// Only a build with MARMOT_TRACE_V2=1 in the environment of marmot build has
// the tracer (the marmot_tracev2 tag, and the trace build of the runtime
// core, build/lib/tracev2). Every other build has the stubs in profile.go:
// StartTrace fails, and no binary pays for the tracer.

//go:build marmot_tracev2

package runtime

import "unsafe"

// rtTv2Out matches rt_tv2_out (runtime/core/src/internal.h).
type rtTv2Out struct {
	bufs unsafe.Pointer
	gen  uint64
	ts   uint64 // the generation's clock snapshot: trace clock,
	mono int64  // monotonic ns,
	sec  int64  // and wall clock
	nsec int32
	_    int32
}

//go:linkname rt_tv2_switch rt_tv2_switch
//go:noescape
func rt_tv2_switch(mode int32, out *rtTv2Out) int32

//go:linkname rt_tv2_buf rt_tv2_buf
//go:noescape
func rt_tv2_buf(b unsafe.Pointer, n *int32, next *unsafe.Pointer) unsafe.Pointer

//go:linkname rt_tv2_free rt_tv2_free
func rt_tv2_free(b unsafe.Pointer)

//go:linkname rt_tv2_nstacks rt_tv2_nstacks
func rt_tv2_nstacks(gen uint64) int32

//go:linkname rt_tv2_stack rt_tv2_stack
//go:noescape
func rt_tv2_stack(gen uint64, id int32, pc *uintptr) int32

// Event types and string IDs of the trace format (internal/trace/tracev2)
// and of tracev2.c.
const (
	traceEvEventBatch    = 1
	traceEvStacks        = 2
	traceEvStack         = 3
	traceEvStrings       = 4
	traceEvString        = 5
	traceEvFrequency     = 8
	traceEvSync          = 50
	traceEvClockSnapshot = 51
	traceEvEndOfGen      = 52
	traceStrWait         = 1 // + the WAIT_* reason
	traceStrGosched      = 64
	traceStrYield        = 65
	traceStrFirstFree    = 128
	traceFrames          = 32  // TV2_FRAMES
	traceMaxFrames       = 128 // frames per stack after inlining
	traceMaxBatch        = 60 << 10
	traceClockPerSecond  = 1e9 / 64
	traceDefaultPeriodMS = 1000
	traceHeader          = "go 1.26 trace\x00\x00\x00"
	traceSwitchStart     = 0
	traceSwitchAdvance   = 1
	traceSwitchStop      = 2
)

var tracer struct {
	sem  chan struct{} // held while switching (a parking lock: a switch stops the world)
	on   bool
	sess uint64 // counts StartTrace calls; the ticker of an old session ends

	qlock rtMutex
	queue [][]byte
	ended bool          // the last generation is queued
	wake  chan struct{} // a chunk was queued
	done  chan struct{} // ReadTrace returned nil after the end
}

func traceInit() {
	if tracer.sem == nil {
		tracer.sem = make(chan struct{}, 1)
		tracer.wake = make(chan struct{}, 1)
		tracer.done = make(chan struct{}, 1)
	}
}

// StartTrace enables tracing for the current process.
func StartTrace() error {
	traceInit()
	tracer.sem <- struct{}{}
	defer func() { <-tracer.sem }()
	if tracer.on {
		return errorString("tracing is already enabled")
	}
	var out rtTv2Out
	if rt_tv2_switch(traceSwitchStart, &out) != 0 {
		return errorString("tracing is already enabled")
	}
	tracer.on = true
	tracer.sess++
	rt_lock(&tracer.qlock)
	tracer.ended = false
	tracer.queue = append(tracer.queue[:0], []byte(traceHeader))
	rt_unlock(&tracer.qlock)
	traceNotify()
	go traceTicker(tracer.sess, tracePeriod())
	return nil
}

// tracePeriod is the length of a generation: MARMOT_TRACE_PERIOD
// milliseconds (tests set it short), 1 s by default.
func tracePeriod() int64 {
	ms := int64(0)
	for _, c := range gogetenv("MARMOT_TRACE_PERIOD") {
		if c < '0' || c > '9' {
			ms = 0
			break
		}
		ms = ms*10 + int64(c-'0')
	}
	if ms <= 0 {
		ms = traceDefaultPeriodMS
	}
	return ms * 1e6
}

func traceTicker(sess uint64, period int64) {
	for {
		rt_sleep(period)
		tracer.sem <- struct{}{}
		if !tracer.on || tracer.sess != sess {
			<-tracer.sem
			return
		}
		traceSwitch(traceSwitchAdvance)
		<-tracer.sem
	}
}

// traceSwitch ends the generation (and starts the next unless mode is
// traceSwitchStop) and queues it. tracer.sem is held.
func traceSwitch(mode int32) {
	var out rtTv2Out
	if rt_tv2_switch(mode, &out) != 0 {
		return
	}
	chunks := traceGeneration(&out)
	rt_lock(&tracer.qlock)
	tracer.queue = append(tracer.queue, chunks...)
	if mode == traceSwitchStop {
		tracer.ended = true
	}
	rt_unlock(&tracer.qlock)
	traceNotify()
}

func traceNotify() {
	select {
	case tracer.wake <- struct{}{}:
	default:
	}
}

// StopTrace stops tracing. It returns after ReadTrace has returned all of
// the trace and then nil.
func StopTrace() {
	if tracer.sem == nil {
		return
	}
	tracer.sem <- struct{}{}
	if !tracer.on {
		<-tracer.sem
		return
	}
	traceSwitch(traceSwitchStop)
	tracer.on = false
	<-tracer.sem
	<-tracer.done
}

// ReadTrace returns the next chunk of the trace: the header first, then
// one batch per call, a one-byte end of each generation, and nil after
// StopTrace has queued the last generation and it was read. It blocks
// until a chunk is ready.
func ReadTrace() []byte {
	if tracer.sem == nil {
		return nil
	}
	for {
		rt_lock(&tracer.qlock)
		if len(tracer.queue) > 0 {
			b := tracer.queue[0]
			tracer.queue[0] = nil
			tracer.queue = tracer.queue[1:]
			rt_unlock(&tracer.qlock)
			return b
		}
		if tracer.ended {
			tracer.ended = false
			rt_unlock(&tracer.qlock)
			select {
			case tracer.done <- struct{}{}:
			default:
			}
			return nil
		}
		rt_unlock(&tracer.qlock)
		<-tracer.wake
	}
}

// traceAdvance ends the current generation now (runtime/trace calls it when
// a reader joins or leaves), or stops tracing.
func traceAdvance(stopTrace bool) {
	if stopTrace {
		StopTrace()
		return
	}
	if tracer.sem == nil {
		return
	}
	tracer.sem <- struct{}{}
	if tracer.on {
		traceSwitch(traceSwitchAdvance)
	}
	<-tracer.sem
}

//go:linkname traceClockUnitsPerSecond runtime/trace.runtime_traceClockUnitsPerSecond
func traceClockUnitsPerSecond() uint64 { return traceClockPerSecond }

func traceClockNow() uint64 { return uint64(nanotime()) >> 6 }

// traceEnc appends the trace encoding: uvarints.
type traceEnc struct{ b []byte }

func (e *traceEnc) u(v uint64) {
	for v >= 0x80 {
		e.b = append(e.b, byte(v)|0x80)
		v >>= 7
	}
	e.b = append(e.b, byte(v))
}

func (e *traceEnc) ev(t byte, args ...uint64) {
	e.b = append(e.b, t)
	for _, a := range args {
		e.u(a)
	}
}

// traceBatch is one EvEventBatch batch of payload, from no M.
func traceBatch(gen, ts uint64, payload []byte) []byte {
	var e traceEnc
	e.b = make([]byte, 0, len(payload)+32)
	e.ev(traceEvEventBatch, gen, 0, ts, uint64(len(payload)))
	e.b = append(e.b, payload...)
	return e.b
}

// traceGeneration is the chunks of the generation out describes: the sync
// batch, the strings, the stacks, the event batches of the core, and the
// end of the generation.
func traceGeneration(out *rtTv2Out) [][]byte {
	gen, ts := out.gen, out.ts
	var chunks [][]byte
	var s traceEnc
	s.ev(traceEvSync)
	s.ev(traceEvFrequency, traceClockPerSecond)
	s.ev(traceEvClockSnapshot, ts, uint64(out.mono), uint64(out.sec), uint64(out.nsec))
	chunks = append(chunks, traceBatch(gen, ts, s.b))

	// Strings: the reasons, then the names of the stacks' frames.
	strs := map[string]uint64{}
	var st traceEnc
	st.ev(traceEvStrings)
	addString := func(id uint64, v string) {
		if len(st.b)+len(v)+24 > traceMaxBatch {
			chunks = append(chunks, traceBatch(gen, ts, st.b))
			st.b = nil
			st.ev(traceEvStrings)
		}
		st.ev(traceEvString, id, uint64(len(v)))
		st.b = append(st.b, v...)
	}
	for i := uint64(0); i < traceStrGosched-traceStrWait; i++ {
		name := "waiting"
		if i < uint64(len(waitReasonNames)) && waitReasonNames[i] != "" {
			name = waitReasonNames[i]
		}
		addString(traceStrWait+i, name)
	}
	addString(traceStrGosched, "runtime.Gosched")
	addString(traceStrYield, "preempted")
	next := uint64(traceStrFirstFree)
	stringID := func(v string) uint64 {
		if id, ok := strs[v]; ok {
			return id
		}
		id := next
		next++
		strs[v] = id
		addString(id, v)
		return id
	}

	var sk traceEnc
	sk.ev(traceEvStacks)
	var pcs [traceFrames]uintptr
	var fr [maxInlineDepth]pcInfo
	type frame struct {
		pc             uintptr
		fn, file, line uint64
	}
	frames := make([]frame, 0, traceMaxFrames)
	n := rt_tv2_nstacks(gen)
	for id := int32(1); id <= n; id++ {
		k := rt_tv2_stack(gen, id, &pcs[0])
		frames = frames[:0]
		for _, pc := range pcs[:k] {
			nf := pcFrames(pc-1, fr[:])
			for _, info := range fr[:nf] {
				if info.line == 0 && info.file == "" || len(frames) == traceMaxFrames {
					continue // a frame of the C runtime core
				}
				frames = append(frames, frame{pc, stringID(info.namePrefix + info.name), stringID(info.filePrefix + info.file), uint64(info.line)})
			}
		}
		if len(frames) == 0 {
			var pc uintptr
			if k > 0 {
				pc = pcs[0]
			}
			frames = append(frames, frame{pc | 1, stringID("runtime"), stringID("runtime"), 1})
		}
		if len(sk.b)+len(frames)*40+24 > traceMaxBatch {
			chunks = append(chunks, traceBatch(gen, ts, sk.b))
			sk.b = nil
			sk.ev(traceEvStacks)
		}
		sk.ev(traceEvStack, uint64(id), uint64(len(frames)))
		for _, f := range frames {
			sk.u(uint64(f.pc))
			sk.u(f.fn)
			sk.u(f.file)
			sk.u(f.line)
		}
	}
	chunks = append(chunks, traceBatch(gen, ts, st.b), traceBatch(gen, ts, sk.b))

	// The core's batches.
	for b := out.bufs; b != nil; {
		var k int32
		var nb unsafe.Pointer
		data := rt_tv2_buf(b, &k, &nb)
		c := make([]byte, k)
		copy(c, unsafe.Slice((*byte)(data), k))
		chunks = append(chunks, c)
		rt_tv2_free(b)
		b = nb
	}
	return append(chunks, []byte{traceEvEndOfGen})
}
