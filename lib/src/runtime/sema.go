// Semaphores and notify lists for package sync and internal/poll. Pure
// file.
//
// A semaphore is a uint32 counter in the caller's memory. semacquire
// decrements it, blocking while it is zero; semrelease increments it or
// hands the unit directly to a waiter. Waiters are kept in a fixed table of
// buckets hashed by address, each with its own lock.

package runtime

import "unsafe"

const semTabSize = 251

type semaRoot struct {
	lock mutex
	head *sudog // waiters, all addresses of this bucket
	tail *sudog
}

var semtable [semTabSize]semaRoot

func semroot(addr *uint32) *semaRoot {
	return &semtable[(uintptr(unsafe.Pointer(addr))>>3)%semTabSize]
}

// cansemacquire decrements *addr if it is positive.
func cansemacquire(addr *uint32) bool {
	for {
		v := atomicLoad32(addr)
		if v == 0 {
			return false
		}
		if atomicCas32(addr, v, v-1) {
			return true
		}
	}
}

// semacquire1 waits until *addr > 0 and decrements it. lifo puts the
// waiter at the head of the queue (used by sync.Mutex for waiters that have
// already waited once).
func semacquire1(addr *uint32, lifo bool) {
	if cansemacquire(addr) {
		return
	}
	root := semroot(addr)
	lock(&root.lock)
	if cansemacquire(addr) {
		unlock(&root.lock)
		return
	}
	gp := getg()
	s := acquireSudog(gp)
	s.g = gp
	s.elem = unsafe.Pointer(addr)
	if lifo && root.head != nil {
		s.next = root.head
		root.head.prev = s
		root.head = s
	} else {
		s.prev = root.tail
		if root.tail != nil {
			root.tail.next = s
		} else {
			root.head = s
		}
		root.tail = s
	}
	goparkunlock(&root.lock)
	// semrelease handed the unit to this waiter.
	releaseSudog(gp, s)
}

// semrelease1 increments *addr, or wakes one waiter on addr and gives it
// the unit.
func semrelease1(addr *uint32) {
	root := semroot(addr)
	lock(&root.lock)
	for s := root.head; s != nil; s = s.next {
		if s.elem != unsafe.Pointer(addr) {
			continue
		}
		if s.prev != nil {
			s.prev.next = s.next
		} else {
			root.head = s.next
		}
		if s.next != nil {
			s.next.prev = s.prev
		} else {
			root.tail = s.prev
		}
		s.next = nil
		s.prev = nil
		gp := s.g
		unlock(&root.lock)
		goready(gp)
		return
	}
	atomicXadd32(addr, 1)
	unlock(&root.lock)
}

// notifyList is the runtime side of sync.notifyList (sync.Cond). The
// layout must match package sync: wait uint32, notify uint32, a word for
// the lock, head and tail pointers (32 bytes).
type notifyList struct {
	wait   uint32 // ticket of the next waiter, updated atomically
	notify uint32 // ticket of the next waiter to notify
	lock   mutex  // at most one word
	head   *sudog
	tail   *sudog
}

// less reports whether ticket a comes before b, with wrap around.
func less(a, b uint32) bool {
	return int32(a-b) < 0
}

func notifyListAdd(l *notifyList) uint32 {
	return atomicXadd32(&l.wait, 1) - 1
}

func notifyListWait(l *notifyList, t uint32) {
	lock(&l.lock)
	if less(t, l.notify) {
		unlock(&l.lock)
		return
	}
	gp := getg()
	s := acquireSudog(gp)
	s.g = gp
	s.ticket = t
	if l.tail == nil {
		l.head = s
	} else {
		l.tail.next = s
	}
	l.tail = s
	goparkunlock(&l.lock)
	releaseSudog(gp, s)
}

func notifyListNotifyAll(l *notifyList) {
	if atomicLoad32(&l.wait) == atomicLoad32(&l.notify) {
		return
	}
	lock(&l.lock)
	s := l.head
	l.head = nil
	l.tail = nil
	atomicStore32(&l.notify, atomicLoad32(&l.wait))
	unlock(&l.lock)
	for s != nil {
		next := s.next
		s.next = nil
		goready(s.g)
		s = next
	}
}

func notifyListNotifyOne(l *notifyList) {
	if atomicLoad32(&l.wait) == atomicLoad32(&l.notify) {
		return
	}
	lock(&l.lock)
	t := l.notify
	if t == atomicLoad32(&l.wait) {
		unlock(&l.lock)
		return
	}
	atomicStore32(&l.notify, t+1)
	for p, s := (*sudog)(nil), l.head; s != nil; p, s = s, s.next {
		if s.ticket == t {
			n := s.next
			if p != nil {
				p.next = n
			} else {
				l.head = n
			}
			if n == nil {
				l.tail = p
			}
			unlock(&l.lock)
			s.next = nil
			goready(s.g)
			return
		}
	}
	unlock(&l.lock)
}

func notifyListCheck(sz uintptr) {
	if sz != unsafe.Sizeof(notifyList{}) {
		throw("bad notifyList size - sync and runtime disagree")
	}
}
