package model

// runq: the per-P run queue of runtime/core sched.c (runqput, runqputslow, runqget,
// runqgrab, runqsteal; RUNQ_SIZE 256, here a parameter) with one owner and one thief, each
// atomic access one step (sequential consistency). The owner runs a script: 'p' put, 'n'
// put into runnext, 'g' get and run. The thief tries to steal (stealnext true) and runs
// what it gets. At the end everything left is drained. Checked: every goroutine runs exactly
// once (none lost, none run twice).

import (
	"fmt"
	"testing"
)

type rq struct {
	head, tail int
	ring       []int // goroutine ids, 0 empty
	next       int
}

type rqstate struct {
	size    int
	p       [2]rq // p[0] owner's P, p[1] thief's P
	glob    []int
	ran     []int // run counts per goroutine id (1-based)
	script  string
	opc     int // owner: position in script
	ost     int // owner sub-step
	oh, ot  int // owner locals
	og      int
	nextID  int
	tpc     int // thief state
	th, ttl int
	tn, ti  int
	tsteals int // steal attempts left
	tnext   int
	batch   []int
	mutant  bool // the thief moves the victim's head without the CAS: must be caught
}

func (s *rqstate) clone() *rqstate {
	t := *s
	for i := range t.p {
		t.p[i].ring = append([]int(nil), s.p[i].ring...)
	}
	t.glob = append([]int(nil), s.glob...)
	t.ran = append([]int(nil), s.ran...)
	t.batch = append([]int(nil), s.batch...)
	return &t
}

func (s *rqstate) key() string { return fmt.Sprint(*s) }

func (s *rqstate) run(g int) { s.ran[g]++ }

// owner steps (runqput / runqputslow / runqget on p[0])
func (s *rqstate) ownerStep() (*rqstate, bool) {
	if s.opc >= len(s.script) {
		return nil, false
	}
	t := s.clone()
	q := &t.p[0]
	op := s.script[s.opc]
	done := func() { t.opc++; t.ost = 0 }
	switch op {
	case 'p', 'n':
		switch t.ost {
		case 0: // new goroutine g
			t.nextID++
			t.og = t.nextID
			if op == 'n' {
				t.ost = 1
			} else {
				t.ost = 2
			}
		case 1: // old := exchange(runnext, g)
			old := q.next
			q.next = t.og
			if old == 0 {
				done()
			} else {
				t.og = old
				t.ost = 2
			}
		case 2: // h := load head
			t.oh = q.head
			t.ost = 3
		case 3: // t := load tail
			t.ot = q.tail
			if t.ot-t.oh < s.size {
				t.ost = 4
			} else {
				t.ost = 6 // runqputslow
			}
		case 4: // store slot
			q.ring[t.ot%s.size] = t.og
			t.ost = 5
		case 5: // store tail
			q.tail = t.ot + 1
			done()
		case 6: // runqputslow: read half the slots, then CAS head
			n := (t.ot - t.oh) / 2
			t.batch = nil
			for i := 0; i < n; i++ {
				t.batch = append(t.batch, q.ring[(t.oh+i)%s.size])
			}
			t.ost = 7
		case 7:
			n := (t.ot - t.oh) / 2
			if q.head != t.oh {
				t.ost = 2 // CAS failed: retry runqput
				break
			}
			q.head = t.oh + n
			t.glob = append(t.glob, t.batch...)
			t.glob = append(t.glob, t.og)
			done()
		}
	case 'g':
		switch t.ost {
		case 0: // runnext CAS
			if q.next != 0 {
				t.run(q.next)
				q.next = 0
				done()
				break
			}
			t.ost = 1
		case 1:
			t.oh = q.head
			t.ost = 2
		case 2:
			t.ot = q.tail
			if t.ot == t.oh {
				done() // empty
				break
			}
			t.ost = 3
		case 3: // read slot
			t.og = q.ring[t.oh%s.size]
			t.ost = 4
		case 4: // CAS head
			if q.head == t.oh {
				q.head = t.oh + 1
				t.run(t.og)
				done()
			} else {
				t.ost = 1
			}
		}
	}
	return t, true
}

// thief steps: runqsteal(p[1], victim p[0], stealnext true), then run the result and drain
// its own queue
func (s *rqstate) thiefStep() (*rqstate, bool) {
	t := s.clone()
	v := &t.p[0]
	me := &t.p[1]
	switch t.tpc {
	case 0: // start a steal: t := load own tail
		if t.tsteals == 0 {
			return nil, false
		}
		t.tsteals--
		t.ttl = me.tail
		t.tpc = 1
	case 1: // grab: h := load victim head
		t.th = v.head
		t.tpc = 2
	case 2: // tl := load victim tail
		n := v.tail - t.th
		n = n - n/2
		t.tn = n
		if n == 0 {
			t.tpc = 3 // try runnext
		} else if n > s.size/2 {
			t.tpc = 1 // inconsistent snapshot
		} else {
			t.ti = 0
			t.tpc = 5
		}
	case 3: // next := load victim runnext
		t.tnext = v.next
		if t.tnext == 0 {
			t.tpc = 0 // nothing to steal
		} else {
			t.tpc = 4
		}
	case 4: // CAS runnext
		if v.next != t.tnext {
			t.tpc = 1
			break
		}
		v.next = 0
		me.ring[t.ttl%s.size] = t.tnext
		t.tn = 1
		t.tpc = 7
	case 5: // copy one slot
		if t.ti < t.tn {
			me.ring[(t.ttl+t.ti)%s.size] = v.ring[(t.th+t.ti)%s.size]
			t.ti++
			break
		}
		t.tpc = 6
	case 6: // CAS victim head
		if v.head != t.th && !t.mutant {
			t.tpc = 1
			break
		}
		v.head = t.th + t.tn
		t.tpc = 7
	case 7: // runqsteal: run the last one, publish the rest
		n := t.tn - 1
		g := me.ring[(t.ttl+n)%s.size]
		if n > 0 {
			me.tail = t.ttl + n
		}
		t.run(g)
		t.tpc = 0
	}
	return t, true
}

// drain at the end: owner's queue, thief's queue, global queue
func (s *rqstate) drain() {
	for i := range s.p {
		q := &s.p[i]
		if q.next != 0 {
			s.run(q.next)
			q.next = 0
		}
		for ; q.head != q.tail; q.head++ {
			s.run(q.ring[q.head%s.size])
		}
	}
	for _, g := range s.glob {
		s.run(g)
	}
	s.glob = nil
}

func runqCheck(size int, script string, steals int) (int, string) {
	return runqRun(size, script, steals, false)
}

func runqRun(size int, script string, steals int, mutant bool) (int, string) {
	s0 := &rqstate{size: size, script: script, tsteals: steals, mutant: mutant}
	for i := range s0.p {
		s0.p[i].ring = make([]int, size)
	}
	n := 0
	for _, c := range script {
		if c == 'p' || c == 'n' {
			n++
		}
	}
	s0.ran = make([]int, n+1)
	seen := map[string]bool{}
	stack := []*rqstate{s0}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		k := s.key()
		if seen[k] {
			continue
		}
		seen[k] = true
		moved := false
		if t, ok := s.ownerStep(); ok {
			stack = append(stack, t)
			moved = true
		}
		if t, ok := s.thiefStep(); ok {
			stack = append(stack, t)
			moved = true
		}
		if !moved && s.tpc == 0 {
			f := s.clone()
			f.drain()
			for g := 1; g <= n; g++ {
				if f.ran[g] != 1 {
					return len(seen), fmt.Sprintf("goroutine %d ran %d times: %s", g, f.ran[g], k)
				}
			}
		}
	}
	return len(seen), ""
}

func TestRunq(t *testing.T) {
	for _, c := range []struct {
		size   int
		script string
		steals int
	}{
		{4, "pppgg", 2},
		{4, "pppppgg", 2},
		{4, "npnpgg", 2},
		{4, "ppppppg", 3},
		{2, "pppng", 3},
	} {
		n, err := runqCheck(c.size, c.script, c.steals)
		t.Logf("size %d, owner %q, %d steals: %d states", c.size, c.script, c.steals, n)
		if err != "" {
			t.Fatal(err)
		}
	}
}

func TestRunqMutantCaught(t *testing.T) {
	if _, err := runqRun(4, "pppgg", 2, true); err == "" {
		t.Fatal("a steal without the head CAS was not caught")
	} else {
		t.Log("caught:", err)
	}
}
