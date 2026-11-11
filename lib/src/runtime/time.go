// Clocks, sleep and timers for package time, on the C core's one-shot
// timers. Compiled only by marmot.

package runtime

import "unsafe"

//go:linkname time_Sleep time.Sleep
func time_Sleep(ns int64) {
	if ns <= 0 {
		return
	}
	rt_sleep(ns)
}

//go:linkname time_now time.now
func time_now() (sec int64, nsec int32, mono int64) {
	rt_walltime(&sec, &nsec)
	return sec, nsec, rt_nanotime()
}

//go:linkname time_runtimeNow time.runtimeNow
func time_runtimeNow() (sec int64, nsec int32, mono int64) {
	return time_now()
}

//go:linkname time_runtimeNano time.runtimeNano
func time_runtimeNano() int64 { return rt_nanotime() }

//go:linkname time_runtimeIsBubbled time.runtimeIsBubbled
func time_runtimeIsBubbled() bool { return false }

// timeTimer is the runtime side of a time.Timer. Its first two fields are
// time.Timer (C and initTimer); package time only sees that prefix.
type timeTimer struct {
	c         unsafe.Pointer // time.Timer.C
	initTimer bool

	mu     mutex
	when   int64 // monotonic nanoseconds
	period int64 // > 0 for tickers
	f      func(arg any, seq uintptr, delta int64)
	arg    any
	seq    uintptr // incremented on every stop and reset
	handle unsafe.Pointer
	ch     *hchan // the timer channel, nil for AfterFunc timers
}

// timerToken is the argument of one rt_timer_start: the timer and the seq
// value it was started with, so that late firings of stopped timers are
// ignored.
type timerToken struct {
	t   *timeTimer
	seq uintptr
}

//go:linkname time_newTimer time.newTimer
func time_newTimer(when, period int64, f func(any, uintptr, int64), arg any, cp unsafe.Pointer) *timeTimer {
	t := new(timeTimer)
	t.initTimer = true
	t.f = f
	t.arg = arg
	t.ch = (*hchan)(cp)
	lock(&t.mu)
	t.start(when, period)
	unlock(&t.mu)
	return t
}

// start arms t. t.mu is held.
func (t *timeTimer) start(when, period int64) {
	t.when = when
	t.period = period
	tok := &timerToken{t: t, seq: t.seq}
	t.handle = rt_timer_start(when, timerFirePC, unsafe.Pointer(tok))
}

// stop disarms t and reports whether it was pending. t.mu is held.
func (t *timeTimer) stop() bool {
	pending := false
	if t.handle != nil {
		pending = rt_timer_stop(t.handle) == 1
		t.handle = nil
	}
	t.seq++
	if t.ch != nil && t.ch.dataqsiz != 0 {
		// Drop a value sent by an earlier firing, so that no stale time
		// is received after Stop or Reset (Go 1.23 timer semantics).
		for t.ch.ringPop(nil) {
		}
	}
	return pending
}

// timerFire runs on a C core system goroutine when a timer expires.
func timerFire(p unsafe.Pointer) {
	tok := (*timerToken)(p)
	t := tok.t
	lock(&t.mu)
	if tok.seq != t.seq || t.handle == nil {
		unlock(&t.mu)
		return
	}
	now := rt_nanotime()
	delta := now - t.when
	seq := t.seq
	t.handle = nil
	if t.period > 0 {
		next := t.when + t.period
		if next < now {
			next += t.period * ((now - next) / t.period)
			if next < now {
				next += t.period
			}
		}
		t.start(next, t.period)
	}
	// f is sendTime (a non-blocking channel send) or goFunc (starts a
	// goroutine); neither blocks, so it runs under t.mu, which orders it
	// with Stop and Reset.
	t.f(t.arg, seq, delta)
	unlock(&t.mu)
}

//go:linkname time_stopTimer time.stopTimer
func time_stopTimer(t *timeTimer) bool {
	lock(&t.mu)
	pending := t.stop()
	unlock(&t.mu)
	return pending
}

//go:linkname time_resetTimer time.resetTimer
func time_resetTimer(t *timeTimer, when, period int64) bool {
	lock(&t.mu)
	pending := t.stop()
	t.start(when, period)
	unlock(&t.mu)
	return pending
}
