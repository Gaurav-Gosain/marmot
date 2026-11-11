// Per-goroutine state of the Go runtime.
//
// The C core keeps one pointer-sized slot per goroutine (rt_gslot). The Go
// runtime stores a *g there, allocated on first use. The C core treats the
// slot as a GC root, so everything reachable from g stays alive.
//
// Pure file.

package runtime

import "unsafe"

type g struct {
	// handle is the C core rt_g of this goroutine (an opaque host handle in
	// lib/hosttest).
	handle unsafe.Pointer
	// goid is a small positive number, 1 for the main goroutine.
	goid int64
	// printlock is the recursion depth of printlock.
	printlock int32
	// rand is the state of the per-goroutine random generator.
	rand uint64

	// Defer and panic state. See panic.go.
	_defer *_defer
	_panic *_panic

	// param passes a value from the waker to a parked goroutine (channel
	// operations and select).
	param unsafe.Pointer
	// waiting is the list of sudogs this goroutine is blocked on in select.
	waiting *sudog
	// selectDone is set to 1 by the first channel that wins a select.
	selectDone uint32
	// sudogcache is a free list of sudogs owned by this goroutine.
	sudogcache *sudog
	// FIPS 140 service indicator and bypass flag (crypto packages).
	fipsIndicator  uint8
	fipsOnlyBypass bool
}

// sudog is a goroutine waiting on a channel.
type sudog struct {
	g    *g
	next *sudog
	prev *sudog
	// elem is the data element (may point into the goroutine stack).
	elem unsafe.Pointer
	c    *hchan
	// waitlink chains the sudogs of one select.
	waitlink *sudog
	// isSelect marks a sudog that belongs to a select.
	isSelect bool
	// success is true if the channel operation completed because of a
	// value exchange, false if the channel was closed.
	success bool
	// caseIndex is the select case this sudog stands for.
	caseIndex int
	// ticket is the notify list ticket (sync.Cond).
	ticket uint32
}

// cheaprand returns a pseudo random number from the per-goroutine state
// (wyrand).
func cheaprand() uint32 {
	gp := getg()
	gp.rand += 0xa0761d6478bd642f
	hi, lo := mul64(gp.rand, gp.rand^0xe7037ed1a0b428db)
	return uint32(hi ^ lo)
}

func cheaprand64() uint64 {
	return uint64(cheaprand())<<32 | uint64(cheaprand())
}

// cheaprandn returns a number in [0, n).
func cheaprandn(n uint32) uint32 {
	return uint32((uint64(cheaprand()) * uint64(n)) >> 32)
}

// mul64 returns the 128 bit product of x and y.
func mul64(x, y uint64) (hi, lo uint64) {
	const mask32 = 1<<32 - 1
	x0 := x & mask32
	x1 := x >> 32
	y0 := y & mask32
	y1 := y >> 32
	w0 := x0 * y0
	t := x1*y0 + w0>>32
	w1 := t & mask32
	w2 := t >> 32
	w1 += x0 * y1
	hi = x1*y1 + w2 + w1>>32
	lo = x * y
	return
}
