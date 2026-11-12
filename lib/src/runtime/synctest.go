// testing/synctest bubbles. Compiled only by marmot.
//
// Run starts f in a new goroutine that belongs to a bubble: a C core
// goroutine group (rt_group) plus a fake clock. Goroutines started inside
// the bubble join it. Parks on channels, select, sync.Cond and
// WaitGroup.Wait count as durably blocked (rt_park_durable); mutex waits,
// system calls and I/O do not.
//
// The goroutine that called Run drives the bubble from outside. The C core
// calls bubbleIdle when no member of the group is running; the driver then
// wakes the goroutine blocked in Wait if there is one, else advances the
// fake clock to the earliest bubble timer and fires the timers that are
// due, else reports a deadlock. Run returns when no member is left.

package runtime

import "unsafe"

//go:linkname rt_group_new rt_group_new
func rt_group_new(onIdle unsafe.Pointer, arg unsafe.Pointer) unsafe.Pointer

//go:linkname rt_group_join rt_group_join
func rt_group_join(grp unsafe.Pointer)

//go:linkname rt_park_durable rt_park_durable
func rt_park_durable(unlock unsafe.Pointer, lock unsafe.Pointer)

//go:linkname rt_group_counts rt_group_counts
//go:noescape
func rt_group_counts(grp unsafe.Pointer, running, alive *int64)

//go:linkname rt_group_free rt_group_free
func rt_group_free(grp unsafe.Pointer)

// bubbleEpoch is the fake clock at the start of a bubble: midnight UTC,
// 2000-01-01, as in gc.
const bubbleEpoch = 946684800 * 1e9

type bubble struct {
	grp    unsafe.Pointer // the C core group of the members
	driver *g             // the goroutine in Run
	// driverParked is 1 while the driver is parked waiting for the group
	// to go idle; whoever changes it to 0 readies the driver.
	driverParked uint32
	rootDone     uint32
	// pending counts goroutines started from outside the group (the root,
	// timer goroutines) that have not joined it yet; the group may look
	// empty or idle until they do.
	pending uint32

	mu     mutex
	now    int64 // fake monotonic and wall clock, nanoseconds
	timers []bubbleTimer
	waiter *g        // the goroutine blocked in Wait, if any
	assoc  []uintptr // keys of bubbleAssoc entries of this bubble
}

// bubbleTimer is a pending timer of a bubble: a time.Timer (t) or a
// sleeping goroutine (sleeper).
type bubbleTimer struct {
	when    int64
	t       *timeTimer
	seq     uintptr
	sleeper *g
}

// bubbleStart is the closure argument of a goroutine started in a bubble.
type bubbleStart struct {
	fn   func()
	b    *bubble
	join bool // the starter is not a member (the driver firing a timer)
}

var (
	bubbleIdlePC       unsafe.Pointer
	bubbleDriverParkPC unsafe.Pointer
	gostartBubblePC    unsafe.Pointer
)

func bubbleInit() {
	f1 := bubbleIdle
	bubbleIdlePC = funcPC(unsafe.Pointer(&f1))
	f2 := bubbleDriverParkCommit
	bubbleDriverParkPC = funcPC(unsafe.Pointer(&f2))
	f3 := gostartBubble
	gostartBubblePC = funcPC(unsafe.Pointer(&f3))
}

// bubbleIdle runs on a scheduler thread when no member of the group is
// running (park callback rules: no allocation, no blocking).
func bubbleIdle(p unsafe.Pointer) {
	b := (*bubble)(p)
	if atomicCas32(&b.driverParked, 1, 0) {
		rt_ready(b.driver.handle)
	}
}

// bubbleDriverParkCommit runs after the driver is off its stack: it
// publishes that the driver waits, and wakes it at once if the group went
// idle before that.
func bubbleDriverParkCommit(p unsafe.Pointer) {
	b := (*bubble)(p)
	atomicStore32(&b.driverParked, 1)
	var running, alive int64
	rt_group_counts(b.grp, &running, &alive)
	if running == 0 && atomicLoad32(&b.pending) == 0 && atomicCas32(&b.driverParked, 1, 0) {
		rt_ready(b.driver.handle)
	}
}

// newprocBubble starts fn in a goroutine of bubble b.
func newprocBubble(fn func(), b *bubble, join bool) {
	s := &bubbleStart{fn: fn, b: b, join: join}
	if join {
		atomicXadd32(&b.pending, 1)
	}
	rt_go(gostartBubblePC, unsafe.Pointer(s))
}

func gostartBubble(p unsafe.Pointer) {
	s := (*bubbleStart)(p)
	gp := getg()
	resetGoroutineState(gp)
	gp.bubble = s.b
	if s.join {
		rt_group_join(s.b.grp)
		atomicXadd32(&s.b.pending, -1)
	}
	s.fn()
}

//go:linkname synctest_Run internal/synctest.Run
func synctest_Run(f func()) {
	gp := getg()
	if gp.bubble != nil {
		panic(plainError("synctest.Run called from within a synctest bubble"))
	}
	if gostartBubblePC == nil {
		bubbleInit()
	}
	b := &bubble{driver: gp, now: bubbleEpoch}
	b.grp = rt_group_new(bubbleIdlePC, unsafe.Pointer(b))
	newprocBubble(func() {
		f()
		atomicStore32(&b.rootDone, 1)
	}, b, true)
	for {
		rt_park(bubbleDriverParkPC, unsafe.Pointer(b))
		var running, alive int64
		rt_group_counts(b.grp, &running, &alive)
		if atomicLoad32(&b.pending) != 0 || running > 0 {
			continue
		}
		if alive == 0 {
			break
		}
		lock(&b.mu)
		if w := b.waiter; w != nil {
			b.waiter = nil
			unlock(&b.mu)
			rt_ready(w.handle)
			continue
		}
		if len(b.timers) > 0 {
			b.now = b.timers[0].when
			var due []bubbleTimer
			for len(b.timers) > 0 && b.timers[0].when <= b.now {
				due = append(due, b.timers[0])
				b.timers = b.timers[1:]
			}
			unlock(&b.mu)
			for _, bt := range due {
				b.fire(bt)
			}
			continue
		}
		unlock(&b.mu)
		if atomicLoad32(&b.rootDone) != 0 {
			panic(plainError("deadlock: main bubble goroutine has exited but blocked goroutines remain"))
		}
		panic(plainError("deadlock: all goroutines in bubble are blocked"))
	}
	rt_group_free(b.grp)
	b.grp = nil
	b.dropAssociations()
}

// fire runs a due bubble timer on the driver goroutine. Goroutines that a
// timer starts (AfterFunc) join the bubble.
func (b *bubble) fire(bt bubbleTimer) {
	if bt.sleeper != nil {
		rt_ready(bt.sleeper.handle)
		return
	}
	t := bt.t
	lock(&t.mu)
	if bt.seq != t.seq || !t.bubblePending {
		unlock(&t.mu)
		return
	}
	t.bubblePending = false
	now := b.now
	delta := now - t.when
	seq := t.seq
	if t.period > 0 {
		next := t.when + t.period
		for next <= now {
			next += t.period
		}
		t.start(next, t.period)
	}
	gp := getg()
	gp.bubbleStart = b
	t.f(t.arg, seq, delta)
	gp.bubbleStart = nil
	unlock(&t.mu)
}

// clock returns the fake time of the bubble.
func (b *bubble) clock() int64 {
	lock(&b.mu)
	now := b.now
	unlock(&b.mu)
	return now
}

// addTimer inserts bt in when order (equal times keep insertion order).
func (b *bubble) addTimer(bt bubbleTimer) {
	i := len(b.timers)
	for i > 0 && b.timers[i-1].when > bt.when {
		i--
	}
	b.timers = append(b.timers, bubbleTimer{})
	copy(b.timers[i+1:], b.timers[i:])
	b.timers[i] = bt
}

// removeTimer drops the pending entry of t and reports whether there was
// one. b.mu is held.
func (b *bubble) removeTimer(t *timeTimer) bool {
	for i := range b.timers {
		if b.timers[i].t == t {
			b.timers = append(b.timers[:i], b.timers[i+1:]...)
			return true
		}
	}
	return false
}

// bubbleSleep is time.Sleep inside a bubble.
func bubbleSleep(b *bubble, ns int64) {
	gp := getg()
	lock(&b.mu)
	b.addTimer(bubbleTimer{when: b.now + ns, sleeper: gp})
	goparkunlockDurable(&b.mu)
}

//go:linkname synctest_Wait internal/synctest.Wait
func synctest_Wait() {
	gp := getg()
	b := gp.bubble
	if b == nil {
		panic(plainError("synctest.Wait not called from within a synctest bubble"))
	}
	lock(&b.mu)
	if b.waiter != nil {
		unlock(&b.mu)
		panic(plainError("synctest.Wait called by multiple goroutines in the same bubble"))
	}
	b.waiter = gp
	goparkunlockDurable(&b.mu)
}

//go:linkname synctest_IsInBubble internal/synctest.IsInBubble
func synctest_IsInBubble() bool { return getg().bubble != nil }

// Object associations (internal/synctest.Associate): the first association
// from inside a bubble ties an object to it, so that WaitGroups and other
// objects know whether they are used from their own bubble. Only heap
// objects are associated. Keys are
// inverted addresses (the table is scanned conservatively and must not keep
// objects alive); a bubble's entries are dropped when its Run returns.

const (
	bubbleAssocUnbubbled = 0
	bubbleAssocCurrent   = 1
	bubbleAssocOther     = 2
)

var (
	bubbleAssocLock mutex
	bubbleAssoc     map[uintptr]*bubble
)

//go:linkname rt_heap_object rt_heap_object
func rt_heap_object(p unsafe.Pointer) int32

//go:linkname synctest_associate internal/synctest.associate
func synctest_associate(p unsafe.Pointer) int {
	if rt_heap_object(p) == 0 {
		// Package variables, stack and C memory are never bubbled, as in
		// gc (only heap objects carry associations).
		return bubbleAssocUnbubbled
	}
	cur := getg().bubble
	lock(&bubbleAssocLock)
	defer unlock(&bubbleAssocLock)
	if b, ok := bubbleAssoc[^uintptr(p)]; ok {
		if b == cur {
			return bubbleAssocCurrent
		}
		return bubbleAssocOther
	}
	if cur == nil {
		return bubbleAssocUnbubbled
	}
	if bubbleAssoc == nil {
		bubbleAssoc = make(map[uintptr]*bubble)
	}
	bubbleAssoc[^uintptr(p)] = cur
	cur.assoc = append(cur.assoc, ^uintptr(p))
	return bubbleAssocCurrent
}

//go:linkname synctest_disassociate internal/synctest.disassociate
func synctest_disassociate(p unsafe.Pointer) {
	lock(&bubbleAssocLock)
	delete(bubbleAssoc, ^uintptr(p))
	unlock(&bubbleAssocLock)
}

//go:linkname synctest_isAssociated internal/synctest.isAssociated
func synctest_isAssociated(p unsafe.Pointer) bool {
	cur := getg().bubble
	lock(&bubbleAssocLock)
	b, ok := bubbleAssoc[^uintptr(p)]
	unlock(&bubbleAssocLock)
	return ok && b == cur
}

// dropAssociations forgets the objects associated with b.
func (b *bubble) dropAssociations() {
	lock(&bubbleAssocLock)
	for _, k := range b.assoc {
		if bubbleAssoc[k] == b {
			delete(bubbleAssoc, k)
		}
	}
	unlock(&bubbleAssocLock)
	b.assoc = nil
}

//go:linkname synctest_acquire internal/synctest.acquire
func synctest_acquire() any {
	if b := getg().bubble; b != nil {
		return b
	}
	return nil
}

//go:linkname synctest_release internal/synctest.release
func synctest_release(b any) {}

//go:linkname synctest_inBubble internal/synctest.inBubble
func synctest_inBubble(b any, f func()) {
	gp := getg()
	old := gp.bubble
	gp.bubble, _ = b.(*bubble)
	defer func() { gp.bubble = old }()
	f()
}
