// Channels. The algorithm follows the gc runtime: a lock per channel, a
// circular buffer, and queues of waiting goroutines (sudogs). A goroutine
// that blocks parks with gopark; the goroutine that completes the operation
// copies the element directly to or from the parked goroutine's memory and
// wakes it with goready. Pure file.

package runtime

import "unsafe"

type hchan struct {
	qcount   uint           // number of elements in the buffer
	dataqsiz uint           // size of the circular buffer
	buf      unsafe.Pointer // dataqsiz elements
	elemsize uintptr
	closed   uint32
	elemtype *_type
	sendx    uint // send index
	recvx    uint // receive index
	recvq    waitq
	sendq    waitq
	lock     mutex
}

type waitq struct {
	first *sudog
	last  *sudog
}

const hchanSize = unsafe.Sizeof(hchan{})

// makechan implements make(chan T, size). elem is T.
func makechan(elem *_type, size int) *hchan {
	if elem.size >= 1<<16 {
		throw("makechan: invalid channel element type")
	}
	mem, overflow := mulUintptr(elem.size, uintptr(size))
	if overflow || mem > maxAlloc-hchanSize || size < 0 {
		panic(plainError("makechan: size out of range"))
	}
	var c *hchan
	switch {
	case mem == 0:
		// Queue or element size is zero.
		c = (*hchan)(mallocConservative(hchanSize))
		c.buf = unsafe.Pointer(c)
	case elem.ptrdata == 0:
		// Elements do not contain pointers: one allocation.
		c = (*hchan)(mallocConservative(hchanSize + mem))
		c.buf = unsafe.Add(unsafe.Pointer(c), hchanSize)
	default:
		c = (*hchan)(mallocConservative(hchanSize))
		c.buf = mallocgc(mem, elem)
	}
	c.elemsize = elem.size
	c.elemtype = elem
	c.dataqsiz = uint(size)
	return c
}

// makechan64 is makechan with an int64 size.
func makechan64(elem *_type, size int64) *hchan {
	if int64(int(size)) != size {
		panic(plainError("makechan: size out of range"))
	}
	return makechan(elem, int(size))
}

// chanbuf returns a pointer to slot i of the buffer.
func chanbuf(c *hchan, i uint) unsafe.Pointer {
	return unsafe.Add(c.buf, uintptr(i)*c.elemsize)
}

// parkunlock is the unlock function passed to gopark for channel locks.
func parkunlock(l unsafe.Pointer) {
	unlock((*mutex)(l))
}

// chansend1 implements c <- *elem.
func chansend1(c *hchan, elem unsafe.Pointer) {
	chansend(c, elem, true)
}

// chansend sends *ep on c. If block is false it returns false instead of
// blocking. A send on a nil channel blocks forever; a send on a closed
// channel panics.
func chansend(c *hchan, ep unsafe.Pointer, block bool) bool {
	if c == nil {
		if !block {
			return false
		}
		goparkForever()
		throw("unreachable")
	}

	lock(&c.lock)

	if c.closed != 0 {
		unlock(&c.lock)
		panic(plainError("send on closed channel"))
	}

	if sg := c.recvq.dequeue(); sg != nil {
		// A receiver is waiting: hand the value over directly.
		send(c, sg, ep)
		unlock(&c.lock)
		return true
	}

	if c.qcount < c.dataqsiz {
		qp := chanbuf(c, c.sendx)
		typedmemmove(c.elemtype, qp, ep)
		c.sendx++
		if c.sendx == c.dataqsiz {
			c.sendx = 0
		}
		c.qcount++
		unlock(&c.lock)
		return true
	}

	if !block {
		unlock(&c.lock)
		return false
	}

	// Block until a receiver takes the value.
	gp := getg()
	mysg := acquireSudog(gp)
	mysg.elem = ep
	mysg.g = gp
	mysg.isSelect = false
	mysg.c = c
	gp.param = nil
	c.sendq.enqueue(mysg)
	goparkunlock(&c.lock)

	closed := !mysg.success
	gp.param = nil
	mysg.c = nil
	releaseSudog(gp, mysg)
	if closed {
		if c.closed == 0 {
			throw("chansend: spurious wakeup")
		}
		panic(plainError("send on closed channel"))
	}
	return true
}

// send completes a send to the waiting receiver sg. c is locked.
func send(c *hchan, sg *sudog, ep unsafe.Pointer) {
	if c.dataqsiz != 0 {
		// The buffer is empty (otherwise nobody would wait to receive).
		// Keep FIFO order: the value goes through the buffer slot.
		c.recvx++
		if c.recvx == c.dataqsiz {
			c.recvx = 0
		}
		c.sendx = c.recvx
	}
	if sg.elem != nil {
		typedmemmove(c.elemtype, sg.elem, ep)
		sg.elem = nil
	}
	gp := sg.g
	gp.param = unsafe.Pointer(sg)
	sg.success = true
	goready(gp)
}

// closechan implements close(c).
func closechan(c *hchan) {
	if c == nil {
		panic(plainError("close of nil channel"))
	}

	lock(&c.lock)
	if c.closed != 0 {
		unlock(&c.lock)
		panic(plainError("close of closed channel"))
	}
	c.closed = 1

	// Collect all waiters, then wake them after releasing the lock.
	var glist *sudog
	for {
		sg := c.recvq.dequeue()
		if sg == nil {
			break
		}
		if sg.elem != nil {
			typedmemclr(c.elemtype, sg.elem)
			sg.elem = nil
		}
		sg.g.param = unsafe.Pointer(sg)
		sg.success = false
		sg.next = glist
		glist = sg
	}
	for {
		sg := c.sendq.dequeue()
		if sg == nil {
			break
		}
		sg.elem = nil
		sg.g.param = unsafe.Pointer(sg)
		sg.success = false
		sg.next = glist
		glist = sg
	}
	unlock(&c.lock)

	for glist != nil {
		sg := glist
		glist = sg.next
		sg.next = nil
		goready(sg.g)
	}
}

// chanrecv1 implements *elem = <-c. elem may be nil if the value is
// discarded.
func chanrecv1(c *hchan, elem unsafe.Pointer) {
	chanrecv(c, elem, true)
}

// chanrecv2 implements *elem, ok = <-c.
func chanrecv2(c *hchan, elem unsafe.Pointer) bool {
	_, received := chanrecv(c, elem, true)
	return received
}

// chanrecv receives from c and writes the value to ep (ep may be nil). If
// block is false and no value is ready it returns (false, false).
// Otherwise selected is true, and received reports whether a value was
// received (false when the channel is closed and drained, in which case
// *ep is zeroed).
func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
	if c == nil {
		if !block {
			return
		}
		goparkForever()
		throw("unreachable")
	}

	lock(&c.lock)

	if c.closed != 0 {
		if c.qcount == 0 {
			unlock(&c.lock)
			if ep != nil {
				typedmemclr(c.elemtype, ep)
			}
			return true, false
		}
	} else {
		if sg := c.sendq.dequeue(); sg != nil {
			recv(c, sg, ep)
			unlock(&c.lock)
			return true, true
		}
	}

	if c.qcount > 0 {
		qp := chanbuf(c, c.recvx)
		if ep != nil {
			typedmemmove(c.elemtype, ep, qp)
		}
		typedmemclr(c.elemtype, qp)
		c.recvx++
		if c.recvx == c.dataqsiz {
			c.recvx = 0
		}
		c.qcount--
		unlock(&c.lock)
		return true, true
	}

	if !block {
		unlock(&c.lock)
		return false, false
	}

	gp := getg()
	mysg := acquireSudog(gp)
	mysg.elem = ep
	mysg.g = gp
	mysg.isSelect = false
	mysg.c = c
	gp.param = nil
	c.recvq.enqueue(mysg)
	goparkunlock(&c.lock)

	success := mysg.success
	gp.param = nil
	mysg.c = nil
	releaseSudog(gp, mysg)
	return true, success
}

// recv completes a receive from the waiting sender sg. c is locked.
func recv(c *hchan, sg *sudog, ep unsafe.Pointer) {
	if c.dataqsiz == 0 {
		if ep != nil {
			typedmemmove(c.elemtype, ep, sg.elem)
		}
	} else {
		// The buffer is full. Take the head of the queue and put the
		// sender's value at the tail, which is the same slot.
		qp := chanbuf(c, c.recvx)
		if ep != nil {
			typedmemmove(c.elemtype, ep, qp)
		}
		typedmemmove(c.elemtype, qp, sg.elem)
		c.recvx++
		if c.recvx == c.dataqsiz {
			c.recvx = 0
		}
		c.sendx = c.recvx
	}
	sg.elem = nil
	gp := sg.g
	gp.param = unsafe.Pointer(sg)
	sg.success = true
	goready(gp)
}

// chanlen implements len(c).
func chanlen(c *hchan) int {
	if c == nil {
		return 0
	}
	return int(c.qcount)
}

// chancap implements cap(c).
func chancap(c *hchan) int {
	if c == nil {
		return 0
	}
	return int(c.dataqsiz)
}

func (q *waitq) enqueue(sgp *sudog) {
	sgp.next = nil
	x := q.last
	if x == nil {
		sgp.prev = nil
		q.first = sgp
		q.last = sgp
		return
	}
	sgp.prev = x
	x.next = sgp
	q.last = sgp
}

// dequeue removes and returns the first waiter that can still proceed.
// Waiters from a select that another channel already won are skipped.
func (q *waitq) dequeue() *sudog {
	for {
		sgp := q.first
		if sgp == nil {
			return nil
		}
		y := sgp.next
		if y == nil {
			q.first = nil
			q.last = nil
		} else {
			y.prev = nil
			q.first = y
			sgp.next = nil
		}

		// A select waiter is woken by at most one channel.
		if sgp.isSelect && !atomicCas32(&sgp.g.selectDone, 0, 1) {
			continue
		}
		return sgp
	}
}

// dequeueSudoG removes sgp from q, if it is there.
func (q *waitq) dequeueSudoG(sgp *sudog) {
	x := sgp.prev
	y := sgp.next
	if x != nil {
		if y != nil {
			x.next = y
			y.prev = x
			sgp.next = nil
			sgp.prev = nil
			return
		}
		x.next = nil
		q.last = x
		sgp.prev = nil
		return
	}
	if y != nil {
		y.prev = nil
		q.first = y
		sgp.next = nil
		return
	}
	if q.first == sgp {
		q.first = nil
		q.last = nil
	}
}

// acquireSudog returns a sudog for gp, from gp's free list when possible.
func acquireSudog(gp *g) *sudog {
	if s := gp.sudogcache; s != nil {
		gp.sudogcache = s.next
		s.next = nil
		return s
	}
	return (*sudog)(mallocConservative(unsafe.Sizeof(sudog{})))
}

// releaseSudog clears s and puts it on gp's free list. gp is the goroutine
// that acquired s; only it uses the list, so no lock is needed.
func releaseSudog(gp *g, s *sudog) {
	*s = sudog{}
	s.next = gp.sudogcache
	gp.sudogcache = s
}

// typedmemmove copies a value of type t from src to dst.
func typedmemmove(t *_type, dst, src unsafe.Pointer) {
	if dst == src || t.size == 0 {
		return
	}
	memmove(dst, src, t.size)
}

// typedmemclr zeroes a value of type t at p.
func typedmemclr(t *_type, p unsafe.Pointer) {
	memclrNoHeapPointers(p, t.size)
}
