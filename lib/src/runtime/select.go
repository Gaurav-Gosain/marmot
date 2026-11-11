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
		j := gp.cheaprandn(uint32(len(pollorder) + 1))
		pollorder = pollorder[:len(pollorder)+1]
		pollorder[len(pollorder)-1] = pollorder[j]
		pollorder[j] = uint16(i)
	}
	norder := len(pollorder)
	lockorder = lockorder[:norder]

	// Lock-free pass over the leading buffered cases in poll order: their
	// rings need no channel lock, so a ready one completes without
	// sorting or locking anything. The pass stops at the first unbuffered
	// case, which the locked passes below handle, so the choice among
	// ready cases still follows the random poll order.
	for _, casei := range pollorder {
		casi := int(casei)
		cas := cases.at(casi)
		c := cas.c
		if c.dataqsiz == 0 {
			break
		}
		if cas.send {
			switch c.ringPush(cas.elem) {
			case ringClosed:
				panic(plainError("send on closed channel"))
			case ringOK:
				if atomicLoad32(&c.recvq.n) != 0 {
					c.notifyOne(&c.recvq)
				}
				return casi, false
			}
			continue
		}
		if c.ringPop(cas.elem) {
			if atomicLoad32(&c.sendq.n) != 0 {
				c.notifyOne(&c.sendq)
			}
			return casi, true
		}
		if c.ringClosedDrained() {
			if cas.elem != nil {
				typedmemclr(c.elemtype, cas.elem)
			}
			return casi, false
		}
	}

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

	for {
		sellock(cases, lockorder)

		// Pass 1: look for a case that can proceed now.
		for _, casei := range pollorder {
			casi := int(casei)
			cas := cases.at(casi)
			c := cas.c
			if c.dataqsiz != 0 {
				// Buffered channel: the lock-free ring.
				if cas.send {
					switch c.ringPush(cas.elem) {
					case ringClosed:
						selunlock(cases, lockorder)
						panic(plainError("send on closed channel"))
					case ringOK:
						w := c.recvq.dequeue()
						selunlock(cases, lockorder)
						if w != nil {
							notifySudog(w)
						}
						return casi, false
					}
				} else {
					if c.ringPop(cas.elem) {
						w := c.sendq.dequeue()
						selunlock(cases, lockorder)
						if w != nil {
							notifySudog(w)
						}
						return casi, true
					}
					if c.ringClosedDrained() {
						selunlock(cases, lockorder)
						if cas.elem != nil {
							typedmemclr(c.elemtype, cas.elem)
						}
						return casi, false
					}
				}
				continue
			}
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
			} else {
				if sg := c.sendq.dequeue(); sg != nil {
					recv(c, sg, cas.elem)
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

		// Pass 2: enqueue on every channel.
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

		// Buffered cases are changed without the lock: now that we are
		// queued (and so will be notified), check them again before
		// parking (Dekker). If one is ready, withdraw and poll again.
		retry := false
		for _, casei := range lockorder {
			cas := cases.at(int(casei))
			c := cas.c
			if c.dataqsiz == 0 {
				continue
			}
			if c.ringIsClosed() || (cas.send && c.ringLen() < int(c.dataqsiz)) || (!cas.send && c.ringLen() > 0) {
				retry = true
				break
			}
		}
		if retry {
			selectWithdraw(gp, cases, nil)
			selunlock(cases, lockorder)
			procyield(10)
			continue
		}

		goparkselect(gp)

		// Pass 3: the winner is in gp.param. Dequeue from the other
		// channels.
		sellock(cases, lockorder)
		gp.selectDone = 0
		win := (*sudog)(gp.param)
		gp.param = nil
		if win == nil {
			throw("selectgo: bad wakeup")
		}
		notified := win.notify
		casi := win.caseIndex
		winCase := cases.at(casi)
		recvOK = win.success
		selectWithdraw(gp, cases, win)
		selunlock(cases, lockorder)
		if notified {
			// A buffered channel changed: poll again.
			continue
		}
		if winCase.send && !recvOK {
			panic(plainError("send on closed channel"))
		}
		if winCase.send {
			recvOK = false
		}
		return casi, recvOK
	}
}

// selectWithdraw removes gp's select sudogs (gp.waiting) from their
// channels, except win (already removed by its waker), and releases them.
// The channels are locked.
func selectWithdraw(gp *g, cases *scase, win *sudog) {
	sglist := gp.waiting
	gp.waiting = nil
	for sg := sglist; sg != nil; {
		next := sg.waitlink
		if sg != win {
			cas := cases.at(sg.caseIndex)
			if cas.send {
				cas.c.sendq.dequeueSudoG(sg)
			} else {
				cas.c.recvq.dequeueSudoG(sg)
			}
		}
		releaseSudog(gp, sg)
		sg = next
	}
}

// selectnbsend implements select { case c <- v: ... default: ... }.
func selectnbsend(c *hchan, elem unsafe.Pointer) bool {
	return chansend(c, elem, false)
}

// selectnbrecv implements select { case v, ok = <-c: ... default: ... }.
func selectnbrecv(elem unsafe.Pointer, c *hchan) (selected, received bool) {
	return chanrecv(c, elem, false)
}
