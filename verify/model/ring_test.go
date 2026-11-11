// Package model holds exhaustive model checks of runtime algorithms, transcribed step by
// step from lib/src/runtime. Each atomic operation of the code is one step; every
// interleaving of the steps of all goroutines is explored (sequential consistency, which
// is what the code's atomics provide). This checks the transcription, not the compiled
// code; the transcription is kept next to the code it follows, line by line.
package model

import (
	"fmt"
	"testing"
)

// ring: chanring.go, ringPush, ringPop and the close bit.

const (
	torn  = -1 // a slot in the middle of a non-atomic copy
	empty = -2 // a cleared slot
)

const closedBit = 1 << 62 // stands for chanClosedBit (positions stay far below it)

type prod struct {
	pc, pos, seq int
	next, left   int // next value to send, values left
	id           int
	closed       bool // the push saw the close bit
}

type cons struct {
	pc, pos, seq int
	left         int
	got          []int
}

type state struct {
	n              int
	enqpos, deqpos int
	seqw, slot     []int
	ps             []prod
	cs             []cons
	pushed         map[int]int // position -> value, recorded at the CAS
	closer         int         // 0: will close, 1: closed, 2: no close
	single         bool        // Vyukov's original encoding (p free, p+1 full): must fail at cap 1
	err            error
}

// the sequence word encoding: doubled (chanring.go) or the original
func (s *state) free(pos int) int {
	if s.single {
		return pos
	}
	return 2 * pos
}

func (s *state) full(pos int) int {
	if s.single {
		return pos + 1
	}
	return 2*pos + 1
}

func (s *state) clone() *state {
	t := *s
	t.seqw = append([]int(nil), s.seqw...)
	t.slot = append([]int(nil), s.slot...)
	t.ps = append([]prod(nil), s.ps...)
	t.cs = make([]cons, len(s.cs))
	for i, c := range s.cs {
		t.cs[i] = c
		t.cs[i].got = append([]int(nil), c.got...)
	}
	t.pushed = map[int]int{}
	for k, v := range s.pushed {
		t.pushed[k] = v
	}
	return &t
}

func (s *state) key() string {
	return fmt.Sprint(s.enqpos, s.deqpos, s.seqw, s.slot, s.ps, s.cs, s.closer)
}

// stepP advances producer i by one atomic step.
func (s *state) stepP(i int) (bool, error) {
	p := &s.ps[i]
	if p.left == 0 || p.closed {
		return false, nil
	}
	switch p.pc {
	case 0: // pos := atomicLoad64(&c.enqpos); if closed bit: ringClosed
		p.pos = s.enqpos
		if p.pos&closedBit != 0 {
			p.closed = true
			return true, nil
		}
		p.pc = 1
	case 1: // seq := atomicLoad64(c.ringSeq(pos)); dif := seq - 2*pos
		p.seq = s.seqw[p.pos%s.n]
		switch d := p.seq - s.free(p.pos); {
		case d == 0:
			p.pc = 2
		case d < 0: // ringFull: the sender blocks and retries later
			p.pc = 0
		default:
			p.pc = 0
		}
	case 2: // atomicCas64(&c.enqpos, pos, pos+1)
		if s.enqpos == p.pos {
			s.enqpos = p.pos + 1
			s.pushed[p.pos] = p.next
			p.pc = 3
		} else {
			p.pc = 0
		}
	case 3: // typedmemmove(slot, ep): first half of a non-atomic copy
		if v := s.slot[p.pos%s.n]; v != empty {
			return false, fmt.Errorf("send into a slot that holds %d", v)
		}
		s.slot[p.pos%s.n] = torn
		p.pc = 4
	case 4: // second half
		s.slot[p.pos%s.n] = p.next
		p.pc = 5
	case 5: // atomicStore64(c.ringSeq(pos), 2*pos+1)
		s.seqw[p.pos%s.n] = s.full(p.pos)
		p.next++
		p.left--
		p.pc = 0
	}
	return true, nil
}

// stepC advances consumer i by one atomic step.
func (s *state) stepC(i int) (bool, error) {
	c := &s.cs[i]
	if c.left == 0 {
		return false, nil
	}
	switch c.pc {
	case 0: // pos := atomicLoad64(&c.deqpos)
		c.pos = s.deqpos
		c.pc = 1
	case 1: // seq := atomicLoad64(c.ringSeq(pos)); dif := seq - (2*pos+1)
		c.seq = s.seqw[c.pos%s.n]
		switch d := c.seq - s.full(c.pos); {
		case d == 0:
			c.pc = 2
		default: // empty (or being written): retry later; or a stale pos: retry
			c.pc = 0
		}
	case 2: // atomicCas64(&c.deqpos, pos, pos+1)
		if s.deqpos == c.pos {
			s.deqpos = c.pos + 1
			c.pc = 3
		} else {
			c.pc = 0
		}
	case 3: // typedmemmove(ep, slot)
		v := s.slot[c.pos%s.n]
		if v == torn || v == empty {
			return false, fmt.Errorf("receive read a slot in state %d at position %d", v, c.pos)
		}
		if want := s.pushed[c.pos]; v != want {
			return false, fmt.Errorf("position %d: received %d, sent %d", c.pos, v, want)
		}
		c.got = append(c.got, v)
		c.pc = 4
	case 4: // typedmemclr(slot)
		s.slot[c.pos%s.n] = empty
		c.pc = 5
	case 5: // atomicStore64(c.ringSeq(pos), 2*(pos+size))
		s.seqw[c.pos%s.n] = s.free(c.pos + s.n)
		c.left--
		c.pc = 0
	}
	return true, nil
}

type result struct {
	states, finals int
	err            error
}

func explore(t *testing.T, s *state, seen map[string]bool, r *result, wantTotal int) {
	k := s.key()
	if r.err != nil || seen[k] {
		return
	}
	seen[k] = true
	r.states++
	moved := false
	for i := range s.ps {
		t2 := s.clone()
		ok, err := t2.stepP(i)
		if err != nil {
			r.err = err
			return
		}
		if ok {
			moved = true
			explore(t, t2, seen, r, wantTotal)
		}
	}
	for i := range s.cs {
		t2 := s.clone()
		ok, err := t2.stepC(i)
		if err != nil {
			r.err = err
			return
		}
		if ok {
			moved = true
			explore(t, t2, seen, r, wantTotal)
		}
	}
	if s.closer == 0 { // close: atomic OR of the bit into enqpos
		t2 := s.clone()
		t2.enqpos |= closedBit
		t2.closer = 1
		moved = true
		explore(t, t2, seen, r, wantTotal)
	}
	_ = moved
	// a state from which nobody can make progress except by spinning is a final one when
	// every producer is done or closed out and every consumer is done
	done := true
	for _, p := range s.ps {
		if p.left > 0 && !p.closed {
			done = false
		}
	}
	for _, c := range s.cs {
		if c.left > 0 {
			done = false
		}
	}
	if done {
		r.finals++
		checkFinal(t, s)
	}
}

// checkFinal: every value was received at most once, received values are what was sent at
// their positions, and each consumer saw positions in increasing order (FIFO).
func checkFinal(t *testing.T, s *state) {
	seen := map[int]bool{}
	for _, c := range s.cs {
		for _, v := range c.got {
			if seen[v] {
				t.Fatalf("value %d received twice", v)
			}
			seen[v] = true
		}
	}
	// FIFO per producer: within one consumer, values of one producer appear in send order
	for _, c := range s.cs {
		last := map[int]int{}
		for _, v := range c.got {
			p := v / 100
			if l, ok := last[p]; ok && v <= l {
				t.Fatalf("consumer saw %d after %d from one producer", v, l)
			}
			last[p] = v
		}
	}
}

func run(t *testing.T, n, producers, perProducer, consumers int, closeToo bool) {
	if r := runModel(t, n, producers, perProducer, consumers, closeToo, false); r.err != nil {
		t.Fatal(r.err)
	} else if r.finals == 0 && !closeToo {
		t.Fatalf("no final state reached")
	}
}

func runModel(t *testing.T, n, producers, perProducer, consumers int, closeToo, single bool) *result {
	s := &state{n: n, pushed: map[int]int{}, closer: 2, single: single}
	if closeToo {
		s.closer = 0
	}
	for i := 0; i < n; i++ {
		s.seqw = append(s.seqw, s.free(i)) // ringInit
		s.slot = append(s.slot, empty)
	}
	total := producers * perProducer
	for p := 0; p < producers; p++ {
		s.ps = append(s.ps, prod{next: 100 * (p + 1), left: perProducer, id: p})
	}
	per := 0
	if consumers > 0 {
		per = total / consumers
	}
	for c := 0; c < consumers; c++ {
		s.cs = append(s.cs, cons{left: per})
	}
	if closeToo {
		// with a close, consumers take only what is certain to be there: none
		for c := range s.cs {
			s.cs[c].left = 0
		}
	}
	r := &result{}
	explore(t, s, map[string]bool{}, r, total)
	t.Logf("cap %d, %d producers x %d, %d consumers, close %v, single %v: %d states, %d final", n, producers, perProducer, consumers, closeToo, single, r.states, r.finals)
	return r
}

// The original encoding cannot tell "full with p" from "free for p+1" at capacity 1; the
// model must find that (otherwise it would not be checking anything).
func TestRingSingleEncodingFails(t *testing.T) {
	r := runModel(t, 1, 2, 2, 1, false, true)
	if r.err == nil {
		t.Fatal("the original encoding at capacity 1 was not caught")
	}
	t.Log("caught:", r.err)
}

func TestRing(t *testing.T) {
	run(t, 1, 2, 2, 2, false)
	run(t, 2, 2, 2, 2, false)
	run(t, 2, 2, 3, 1, false)
	run(t, 3, 3, 1, 3, false)
	run(t, 1, 2, 2, 0, true)
	run(t, 2, 2, 2, 0, true)
}
