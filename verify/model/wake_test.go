package model

// wake: chanring.go, chansendBuf and chanrecvBuf (no close): the wait queues under c.lock
// and the Dekker pattern that excludes lost wake-ups. Ring operations are single atomic
// steps here (ring_test.go checks them at the level of their atomics). A final state in
// which a goroutine is still parked is a lost wake-up: the sends and receives are balanced,
// so every goroutine must finish.

import (
	"fmt"
	"testing"
)

type gor struct {
	send   bool
	pc     int
	left   int
	parked bool
}

type wstate struct {
	n, count int // capacity and elements in the ring
	lock     int // owner, -1 when free
	recvq    []int
	sendq    []int
	gs       []gor
	noDekker bool // the variant without the retry after enqueueing: must lose a wake-up
}

func (s *wstate) clone() *wstate {
	t := *s
	t.recvq = append([]int(nil), s.recvq...)
	t.sendq = append([]int(nil), s.sendq...)
	t.gs = append([]gor(nil), s.gs...)
	return &t
}

func (s *wstate) key() string { return fmt.Sprint(s.count, s.lock, s.recvq, s.sendq, s.gs) }

func remove(q []int, g int) []int {
	for i, x := range q {
		if x == g {
			return append(append([]int(nil), q[:i]...), q[i+1:]...)
		}
	}
	return q
}

// notifyOne(q) as one step: lock, dequeue, unlock, ready the dequeued goroutine
func (s *wstate) notify(q *[]int) bool {
	if s.lock != -1 {
		return false
	}
	if len(*q) > 0 {
		g := (*q)[0]
		*q = (*q)[1:]
		s.gs[g].parked = false
		s.gs[g].pc = 0
	}
	return true
}

func (s *wstate) finishOne(i int) {
	g := &s.gs[i]
	g.left--
	g.pc = 0
}

func (s *wstate) step(i int) bool {
	g := &s.gs[i]
	if g.left == 0 || g.parked {
		return false
	}
	if g.send {
		switch g.pc {
		case 0: // ringPush
			if s.count < s.n {
				s.count++
				g.pc = 1
			} else {
				g.pc = 2
			}
		case 1: // if atomicLoad32(&c.recvq.n) != 0 { notifyOne(&c.recvq) }
			if len(s.recvq) != 0 {
				g.pc = 10
			} else {
				s.finishOne(i)
			}
		case 10:
			if !s.notify(&s.recvq) {
				return false
			}
			s.finishOne(i)
		case 2: // lock(&c.lock)
			if s.lock != -1 {
				return false
			}
			s.lock = i
			g.pc = 3
		case 3: // c.sendq.enqueue(sg)
			s.sendq = append(s.sendq, i)
			g.pc = 4
		case 4: // r := c.ringPush(ep) (the Dekker retry)
			if !s.noDekker && s.count < s.n {
				s.count++
				s.sendq = remove(s.sendq, i)
				s.lock = -1
				g.pc = 1
			} else {
				g.pc = 5
			}
		case 5: // goparkunlock(&c.lock)
			s.lock = -1
			g.parked = true
			g.pc = 0
		}
		return true
	}
	switch g.pc {
	case 0: // ringPop
		if s.count > 0 {
			s.count--
			g.pc = 1
		} else {
			g.pc = 2
		}
	case 1: // if atomicLoad32(&c.sendq.n) != 0 { notifyOne(&c.sendq) }
		if len(s.sendq) != 0 {
			g.pc = 10
		} else {
			s.finishOne(i)
		}
	case 10:
		if !s.notify(&s.sendq) {
			return false
		}
		s.finishOne(i)
	case 2:
		if s.lock != -1 {
			return false
		}
		s.lock = i
		g.pc = 3
	case 3:
		s.recvq = append(s.recvq, i)
		g.pc = 4
	case 4: // if c.ringLen() > 0 || closed { dequeue self; unlock; retry }
		if !s.noDekker && s.count > 0 {
			s.recvq = remove(s.recvq, i)
			s.lock = -1
			g.pc = 0
		} else {
			g.pc = 5
		}
	case 5:
		s.lock = -1
		g.parked = true
		g.pc = 0
	}
	return true
}

func wexplore(s *wstate, seen map[string]bool, lost *string, states *int) {
	k := s.key()
	if seen[k] || *lost != "" {
		return
	}
	seen[k] = true
	*states++
	moved := false
	for i := range s.gs {
		t := s.clone()
		if t.step(i) {
			moved = true
			wexplore(t, seen, lost, states)
		}
	}
	if !moved {
		for _, g := range s.gs {
			if g.left > 0 {
				*lost = "stuck: " + k
				return
			}
		}
	}
}

func wrun(n, senders, perSender, receivers int, noDekker bool) (string, int) {
	s := &wstate{n: n, lock: -1, noDekker: noDekker}
	total := senders * perSender
	for i := 0; i < senders; i++ {
		s.gs = append(s.gs, gor{send: true, left: perSender})
	}
	for i := 0; i < receivers; i++ {
		s.gs = append(s.gs, gor{left: total / receivers})
	}
	lost, states := "", 0
	wexplore(s, map[string]bool{}, &lost, &states)
	return lost, states
}

func TestWake(t *testing.T) {
	for _, c := range [][4]int{{1, 1, 3, 1}, {1, 2, 2, 2}, {2, 2, 2, 1}, {1, 3, 1, 3}, {2, 2, 2, 2}} {
		lost, states := wrun(c[0], c[1], c[2], c[3], false)
		t.Logf("cap %d, %d senders x %d, %d receivers: %d states", c[0], c[1], c[2], c[3], states)
		if lost != "" {
			t.Fatalf("lost wake-up: %s", lost)
		}
	}
}

func TestWakeWithoutDekkerFails(t *testing.T) {
	lost, _ := wrun(1, 1, 3, 1, true)
	if lost == "" {
		t.Fatal("removing the retry after enqueueing lost no wake-up: the model checks nothing")
	}
	t.Log("caught:", lost)
}
