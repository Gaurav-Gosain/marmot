// The ring of a buffered channel: a bounded multi-producer multi-consumer
// queue (D. Vyukov's design) with a sequence number per slot, so that send
// and receive do not take the channel lock when nobody waits.
//
// Waiting goroutines still queue on c.recvq and c.sendq under c.lock, but
// for buffered channels a waiter is only notified, never served: a woken
// receiver or sender retries the ring operation, and a select woken by a
// buffered channel polls its cases again. A waker never has to hand an
// element to a specific waiter, so a lock-free consumer taking that element
// first cannot strand a select.
//
// Lost wake-ups are excluded with a Dekker pattern on sequentially
// consistent atomics: a waiter increments the queue count and then retries
// the ring; an operation that changes the ring then reads the opposite
// queue count and notifies if it is not zero.
//
// close sets chanClosedBit in enqpos with an atomic OR under c.lock, so a
// send is either in the ring before the close (and is received) or sees
// the bit and panics.
//
// Sequence words use a doubled encoding: 2p means the slot is free for
// position p, 2p+1 means it holds the element of position p. Vyukov's
// original (p and p+1) cannot tell "full with p" from "free for p+size"
// when the capacity is 1. Positions stay below 2^62. Pure file.

package runtime

import "unsafe"

const chanClosedBit = 1 << 63

// chanSpinRounds is how many procyield(30) rounds a blocked buffered send
// or receive spins before it parks.
const chanSpinRounds = 16

// Results of ringPush.
const (
	ringOK     = 0
	ringFull   = 1
	ringClosed = 2
)

// Layout of the ring control block. The enqueue position (written by
// senders) and the dequeue position (written by receivers) live on
// separate cache lines (128 bytes covers arm64 and x86 pairs of lines), so
// a sender and a receiver on different threads do not bounce one line on
// every operation. The sequence words follow.
const (
	ringLine   = 128
	ringEnqOff = 0
	ringDeqOff = ringLine
	ringSeqOff = 2 * ringLine
)

// enqp returns the enqueue position: the next position to fill, with
// chanClosedBit set once the channel is closed.
func (c *hchan) enqp() *uint64 { return (*uint64)(unsafe.Add(c.ring, ringEnqOff)) }

// deqp returns the dequeue position: the next position to take.
func (c *hchan) deqp() *uint64 { return (*uint64)(unsafe.Add(c.ring, ringDeqOff)) }

// ringSeq returns the sequence word of the slot for position pos.
func (c *hchan) ringSeq(pos uint64) *uint64 {
	return (*uint64)(unsafe.Add(c.ring, ringSeqOff+uintptr(pos%uint64(c.dataqsiz))*8))
}

// ringSlot returns the element storage of the slot for position pos.
func (c *hchan) ringSlot(pos uint64) unsafe.Pointer {
	return chanbuf(c, uint(pos%uint64(c.dataqsiz)))
}

// ringInit sets up the control block of a new buffered channel, aligned
// to a cache line.
func (c *hchan) ringInit() {
	n := uintptr(c.dataqsiz)
	p := mallocgc(ringSeqOff+n*8+ringLine, nil)
	c.ring = unsafe.Pointer((uintptr(p) + ringLine - 1) &^ (ringLine - 1))
	for i := uintptr(0); i < n; i++ {
		*(*uint64)(unsafe.Add(c.ring, ringSeqOff+i*8)) = 2 * uint64(i)
	}
}

// ringPush copies *ep into the ring.
func (c *hchan) ringPush(ep unsafe.Pointer) int {
	for {
		pos := atomicLoad64(c.enqp())
		if pos&chanClosedBit != 0 {
			return ringClosed
		}
		seq := atomicLoad64(c.ringSeq(pos))
		switch dif := int64(seq - 2*pos); {
		case dif == 0:
			if atomicCas64(c.enqp(), pos, pos+1) {
				typedmemmove(c.elemtype, c.ringSlot(pos), ep)
				atomicStore64(c.ringSeq(pos), 2*pos+1)
				return ringOK
			}
		case dif < 0:
			return ringFull
		}
		// Another sender took this position; retry with a fresh one.
	}
}

// ringPop moves the oldest element to ep (dropped if ep is nil) and
// reports whether there was one. It returns false while the ring is empty
// or the next element is still being written by its sender.
func (c *hchan) ringPop(ep unsafe.Pointer) bool {
	for {
		pos := atomicLoad64(c.deqp())
		seq := atomicLoad64(c.ringSeq(pos))
		switch dif := int64(seq - (2*pos + 1)); {
		case dif == 0:
			if atomicCas64(c.deqp(), pos, pos+1) {
				slot := c.ringSlot(pos)
				if ep != nil {
					typedmemmove(c.elemtype, ep, slot)
				}
				typedmemclr(c.elemtype, slot)
				atomicStore64(c.ringSeq(pos), 2*(pos+uint64(c.dataqsiz)))
				return true
			}
		case dif < 0:
			return false
		}
	}
}

// ringLen returns the number of elements sent and not yet received,
// including elements whose sender is still writing them.
func (c *hchan) ringLen() int {
	enq := atomicLoad64(c.enqp()) &^ chanClosedBit
	deq := atomicLoad64(c.deqp())
	n := int(int64(enq - deq))
	if n < 0 {
		return 0
	}
	if n > int(c.dataqsiz) {
		return int(c.dataqsiz)
	}
	return n
}

// ringClosedDrained reports whether the channel is closed and every
// element sent before the close has been received.
func (c *hchan) ringClosedDrained() bool {
	enq := atomicLoad64(c.enqp())
	return enq&chanClosedBit != 0 && enq&^chanClosedBit == atomicLoad64(c.deqp())
}

// ringClosed reports whether the channel is closed.
func (c *hchan) ringIsClosed() bool {
	return atomicLoad64(c.enqp())&chanClosedBit != 0
}

// notifyOne wakes one waiter of q (c.recvq or c.sendq) if there is one, so
// that it retries. c.lock must not be held.
func (c *hchan) notifyOne(q *waitq) {
	lock(&c.lock)
	sg := q.dequeue()
	unlock(&c.lock)
	if sg != nil {
		notifySudog(sg)
	}
}

// notifySudog wakes the goroutine of sg (already removed from its queue)
// to retry its operation.
func notifySudog(sg *sudog) {
	sg.notify = true
	sg.success = false
	gp := sg.g
	gp.param = unsafe.Pointer(sg)
	goready(gp)
}

// chansendBuf is chansend for buffered channels.
func chansendBuf(c *hchan, ep unsafe.Pointer, block bool) bool {
	spin := 0
	for {
		switch c.ringPush(ep) {
		case ringOK:
			if atomicLoad32(&c.recvq.n) != 0 {
				c.notifyOne(&c.recvq)
			}
			return true
		case ringClosed:
			panic(plainError("send on closed channel"))
		}
		// The ring is full.
		if !block {
			return false
		}
		// Spin before parking (see chanrecvBuf).
		if spin < chanSpinRounds && canSpin(0) {
			spin++
			procyield(30)
			continue
		}
		gp := getg()
		lock(&c.lock)
		sg := acquireSudog(gp)
		sg.g = gp
		sg.c = c
		gp.param = nil
		c.sendq.enqueue(sg)
		// Retry after announcing ourselves (Dekker): a receiver that freed
		// a slot before seeing us left it for us.
		r := c.ringPush(ep)
		if r != ringFull {
			c.sendq.dequeueSudoG(sg)
			unlock(&c.lock)
			releaseSudog(gp, sg)
			if r == ringClosed {
				panic(plainError("send on closed channel"))
			}
			if atomicLoad32(&c.recvq.n) != 0 {
				c.notifyOne(&c.recvq)
			}
			return true
		}
		goparkunlock(&c.lock)
		gp.param = nil
		releaseSudog(gp, sg)
	}
}

// chanrecvBuf is chanrecv for buffered channels.
func chanrecvBuf(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
	spin := 0
	for {
		if c.ringPop(ep) {
			if atomicLoad32(&c.sendq.n) != 0 {
				c.notifyOne(&c.sendq)
			}
			return true, true
		}
		if c.ringClosedDrained() {
			if ep != nil {
				typedmemclr(c.elemtype, ep)
			}
			return true, false
		}
		if c.ringLen() > 0 {
			// A sender is still writing the next element.
			procyield(10)
			continue
		}
		if !block {
			return false, false
		}
		// The ring is empty. A sender on another thread usually adds the
		// next element within a few hundred nanoseconds, and parking
		// plus the sender's wake-up costs microseconds: spin first while
		// other threads run goroutines. 16 rounds (longer than the 4 of
		// sync.Mutex) keep a steady producer from making the receiver
		// park on every element (quiet Linux and macOS A/B).
		if spin < chanSpinRounds && canSpin(0) {
			spin++
			procyield(30)
			continue
		}
		gp := getg()
		lock(&c.lock)
		sg := acquireSudog(gp)
		sg.g = gp
		sg.c = c
		gp.param = nil
		c.recvq.enqueue(sg)
		// Retry after announcing ourselves (Dekker).
		if c.ringLen() > 0 || c.ringIsClosed() {
			c.recvq.dequeueSudoG(sg)
			unlock(&c.lock)
			releaseSudog(gp, sg)
			continue
		}
		goparkunlock(&c.lock)
		gp.param = nil
		releaseSudog(gp, sg)
	}
}

// closechanBuf closes a buffered channel; c.lock is held. It wakes every
// waiter: receivers drain what is left and then see the close, senders
// panic.
func closechanBuf(c *hchan) *sudog {
	atomicOr64(c.enqp(), chanClosedBit)
	var glist *sudog
	for _, q := range [2]*waitq{&c.recvq, &c.sendq} {
		for {
			sg := q.dequeue()
			if sg == nil {
				break
			}
			sg.notify = true
			sg.success = false
			sg.g.param = unsafe.Pointer(sg)
			sg.next = glist
			glist = sg
		}
	}
	return glist
}
