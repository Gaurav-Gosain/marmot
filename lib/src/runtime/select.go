// select. The algorithm follows the gc runtime: poll the cases in a random
// order, and if none is ready, lock every channel (in address order),
// enqueue a sudog on each, park, and on wake-up dequeue from the channels
// that did not fire. Pure file.

package runtime

import "unsafe"

// scase is one case of a select statement, laid out by the compiler in an
// array. elem points to the value to send, or to the receive destination
// (nil when the received value is discarded).
type scase struct {
	c    *hchan
	elem unsafe.Pointer
	send bool
}

func (cases *scase) at(i int) *scase {
	return (*scase)(unsafe.Add(unsafe.Pointer(cases), uintptr(i)*unsafe.Sizeof(scase{})))
}

// selparkcommit unlocks the channels of the select of gp after it parks.
// The sudogs in gp.waiting are in lock order, so equal channels are
// adjacent and are unlocked once.
func selparkcommit(p unsafe.Pointer) {
	gp := (*g)(p)
	var last *hchan
	for sg := gp.waiting; sg != nil; sg = sg.waitlink {
		if sg.c != last && last != nil {
			unlock(&last.lock)
		}
		last = sg.c
	}
	if last != nil {
		unlock(&last.lock)
	}
}

func sellock(cases *scase, lockorder []uint16) {
	var c *hchan
	for _, o := range lockorder {
		c0 := cases.at(int(o)).c
		if c0 != c {
			c = c0
			lock(&c.lock)
		}
	}
}

func selunlock(cases *scase, lockorder []uint16) {
	// Unlock in reverse order, skipping duplicates.
	for i := len(lockorder) - 1; i >= 0; i-- {
		c := cases.at(int(lockorder[i])).c
		if i > 0 && c == cases.at(int(lockorder[i-1])).c {
			continue
		}
		unlock(&c.lock)
	}
}

// selectgo implements the select statement. cases points at ncases scase
// values. If block is false (the select has a default case) and no case is
// ready, it returns chosen = -1. Otherwise chosen is the index of the case
// that ran, and recvOK reports, for a receive case, whether a value was
// received (false if the channel was closed).
func selectgo(cases *scase, ncases int, block bool) (chosen int, recvOK bool) {
	// The poll and lock orders live in a buffer cached on the goroutine:
	// a goroutine runs one select at a time.
	gp := getg()
	order := gp.selorder
	if cap(order) < 2*ncases {
		n := 2 * ncases
		if n < 16 {
			n = 16
		}
		order = unsafe.Slice((*uint16)(mallocgc(uintptr(2*n), nil)), n)
		gp.selorder = order
	}
	order = order[:2*ncases]
	pollorder := order[:0:ncases]
	lockorder := order[ncases:][:0:ncases]

	// Random poll order, skipping nil channels.
	for i := 0; i < ncases; i++ {
		cas := cases.at(i)
		if cas.c == nil {
			cas.elem = nil // allow GC
			continue
		}
		j := cheaprandn(uint32(len(pollorder) + 1))
		pollorder = pollorder[:len(pollorder)+1]
		pollorder[len(pollorder)-1] = pollorder[j]
		pollorder[j] = uint16(i)
	}
	norder := len(pollorder)
	lockorder = lockorder[:norder]

	// Sort the cases by channel address with a heap sort, so that
	// concurrent selects lock channels in the same order.
	for i := range lockorder {
		j := i
		c := cases.at(int(pollorder[i])).c
		for j > 0 && uintptr(unsafe.Pointer(cases.at(int(lockorder[(j-1)/2])).c)) < uintptr(unsafe.Pointer(c)) {
			k := (j - 1) / 2
			lockorder[j] = lockorder[k]
			j = k
		}
		lockorder[j] = pollorder[i]
	}
	for i := len(lockorder) - 1; i >= 0; i-- {
		o := lockorder[i]
		c := cases.at(int(o)).c
		lockorder[i] = lockorder[0]
		j := 0
		for {
			k := j*2 + 1
			if k >= i {
				break
			}
			if k+1 < i && uintptr(unsafe.Pointer(cases.at(int(lockorder[k])).c)) < uintptr(unsafe.Pointer(cases.at(int(lockorder[k+1])).c)) {
				k++
			}
			if uintptr(unsafe.Pointer(c)) < uintptr(unsafe.Pointer(cases.at(int(lockorder[k])).c)) {
				lockorder[j] = lockorder[k]
				j = k
				continue
			}
			break
		}
		lockorder[j] = o
	}

	if norder == 0 {
		if !block {
			return -1, false
		}
		// select {} or only nil channels: block forever.
		goparkForever()
		throw("unreachable")
	}

	sellock(cases, lockorder)

	// Pass 1: look for a case that can proceed now.
	for _, casei := range pollorder {
		casi := int(casei)
		cas := cases.at(casi)
		c := cas.c
		if cas.send {
			if c.closed != 0 {
				selunlock(cases, lockorder)
				panic(plainError("send on closed channel"))
			}
			if sg := c.recvq.dequeue(); sg != nil {
				send(c, sg, cas.elem)
				selunlock(cases, lockorder)
				return casi, false
			}
			if c.qcount < c.dataqsiz {
				typedmemmove(c.elemtype, chanbuf(c, c.sendx), cas.elem)
				c.sendx++
				if c.sendx == c.dataqsiz {
					c.sendx = 0
				}
				c.qcount++
				selunlock(cases, lockorder)
				return casi, false
			}
		} else {
			if sg := c.sendq.dequeue(); sg != nil {
				recv(c, sg, cas.elem)
				selunlock(cases, lockorder)
				return casi, true
			}
			if c.qcount > 0 {
				qp := chanbuf(c, c.recvx)
				if cas.elem != nil {
					typedmemmove(c.elemtype, cas.elem, qp)
				}
				typedmemclr(c.elemtype, qp)
				c.recvx++
				if c.recvx == c.dataqsiz {
					c.recvx = 0
				}
				c.qcount--
				selunlock(cases, lockorder)
				return casi, true
			}
			if c.closed != 0 {
				selunlock(cases, lockorder)
				if cas.elem != nil {
					typedmemclr(c.elemtype, cas.elem)
				}
				return casi, false
			}
		}
	}

	if !block {
		selunlock(cases, lockorder)
		return -1, false
	}

	// Pass 2: enqueue on every channel and park.
	gp.selectDone = 0
	gp.param = nil
	nextp := &gp.waiting
	for _, casei := range lockorder {
		casi := int(casei)
		cas := cases.at(casi)
		c := cas.c
		sg := acquireSudog(gp)
		sg.g = gp
		sg.isSelect = true
		sg.elem = cas.elem
		sg.c = c
		sg.caseIndex = casi
		*nextp = sg
		nextp = &sg.waitlink
		if cas.send {
			c.sendq.enqueue(sg)
		} else {
			c.recvq.enqueue(sg)
		}
	}
	goparkselect(gp)

	// Pass 3: the winner is in gp.param. Dequeue from the other channels.
	sellock(cases, lockorder)
	gp.selectDone = 0
	win := (*sudog)(gp.param)
	gp.param = nil

	casi := -1
	var winCase *scase
	sglist := gp.waiting
	gp.waiting = nil
	for sg := sglist; sg != nil; {
		next := sg.waitlink
		cas := cases.at(sg.caseIndex)
		if sg == win {
			casi = sg.caseIndex
			winCase = cas
			recvOK = sg.success
		} else if cas.send {
			cas.c.sendq.dequeueSudoG(sg)
		} else {
			cas.c.recvq.dequeueSudoG(sg)
		}
		releaseSudog(gp, sg)
		sg = next
	}
	if winCase == nil {
		throw("selectgo: bad wakeup")
	}
	selunlock(cases, lockorder)
	if winCase.send && !recvOK {
		panic(plainError("send on closed channel"))
	}
	if winCase.send {
		recvOK = false
	}
	return casi, recvOK
}

// selectnbsend implements select { case c <- v: ... default: ... }.
func selectnbsend(c *hchan, elem unsafe.Pointer) bool {
	return chansend(c, elem, false)
}

// selectnbrecv implements select { case v, ok = <-c: ... default: ... }.
func selectnbrecv(elem unsafe.Pointer, c *hchan) (selected, received bool) {
	return chanrecv(c, elem, false)
}
