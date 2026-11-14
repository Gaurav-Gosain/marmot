// Clocks, sleep and timers for package time, on the C core's one-shot
// timers. Compiled only by marmot.

package runtime

import "unsafe"

//go:linkname time_Sleep time.Sleep
func time_Sleep(ns int64) {
	if ns <= 0 {
		return
	}
	if b := getg().bubble; b != nil {
		bubbleSleep(b, ns)
		return
	}
	rt_sleep(ns)
}

//go:linkname time_now time.now
func time_now() (sec int64, nsec int32, mono int64) {
	if b := getg().bubble; b != nil {
		now := b.clock()
		return now / 1e9, int32(now % 1e9), now
	}
	rt_walltime(&sec, &nsec)
	return sec, nsec, rt_nanotime()
}

//go:linkname time_runtimeNow time.runtimeNow
func time_runtimeNow() (sec int64, nsec int32, mono int64) {
	return time_now()
}

//go:linkname time_runtimeNano time.runtimeNano
func time_runtimeNano() int64 {
	if b := getg().bubble; b != nil {
		return b.clock()
	}
	return rt_nanotime()
}

//go:linkname time_runtimeIsBubbled time.runtimeIsBubbled
func time_runtimeIsBubbled() bool { return getg().bubble != nil }

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
	seq    uintptr        // incremented on every stop and reset
	handle unsafe.Pointer // the C core timer while armed, else nil
	ct     unsafe.Pointer // the C core timer kept for the next arming (reset allocates nothing)
	ch     *hchan         // the timer channel, nil for AfterFunc timers
	// bubble is the testing/synctest bubble the timer was made in; its
	// pending firing is an entry of the bubble's fake clock, not a C core
	// timer.
	bubble        *bubble
	bubblePending bool
}

//go:linkname time_newTimer time.newTimer
func time_newTimer(when, period int64, f func(any, uintptr, int64), arg any, cp unsafe.Pointer) *timeTimer {
	t := new(timeTimer)
	t.initTimer = true
	t.bubble = getg().bubble
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
	if b := t.bubble; b != nil {
		lock(&b.mu)
		t.bubblePending = true
		b.addTimer(bubbleTimer{when: when, t: t, seq: t.seq})
		unlock(&b.mu)
		return
	}
	// The C core passes the seq of this arming back to timerFire, so a late
	// firing of an earlier arming is ignored.
	t.ct = rt_timer_reset(t.ct, when, lazyPC(&timerFirePC, timerFireCode), unsafe.Pointer(t), t.seq)
	t.handle = t.ct
}

func timerFireCode() unsafe.Pointer {
	tf := timerFire
	return funcPC(unsafe.Pointer(&tf))
}

// stop disarms t and reports whether it was pending. t.mu is held.
func (t *timeTimer) stop() bool {
	pending := false
	if b := t.bubble; b != nil && t.bubblePending {
		lock(&b.mu)
		b.removeTimer(t)
		unlock(&b.mu)
		t.bubblePending = false
		pending = true
	}
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

// timerFire runs on a C core system goroutine when a timer expires, with
// the seq of the arming that fired.
func timerFire(p unsafe.Pointer, armed uintptr) {
	t := (*timeTimer)(p)
	lock(&t.mu)
	if armed != t.seq || t.handle == nil {
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
