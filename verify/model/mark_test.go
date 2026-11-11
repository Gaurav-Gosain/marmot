package model

// mark: parallel marking termination (runtime/core gc.c, drain_parallel, mark_parallel,
// gc_helper, helpers_start, helpers_wait) as a transition system.
//
// Work is abstract: a worker's stack holds units; scanning a unit may push new units (a
// global budget bounds how many). share() moves half of a stack to a full wbuf (one queue,
// wfull says whether it is non-empty). The helper protocol: a helper wakes on a new
// generation, joins (phase_joined++), and if the phase is open increments wactive and
// drains unless wdone; then leaves (phase_joined--). The collector (worker 0) drains, then
// sets wdone and waits for phase_joined == 0.
//
// Checked: (1) whenever wdone is set, no unit is left on any stack or in the queue and no
// worker is in the middle of a scan (marking finished); (2) from every state the end of
// marking (collector past helpers_wait, every helper back asleep) is reachable.

import (
	"fmt"
	"strings"
	"testing"
)

const (
	wLoop    = iota // drain_parallel: top of the loop (take local work or a wbuf)
	wScan           // scanning one unit
	wIdleChk        // the "alone" and "wdone" checks and the wnidle increment
	wSpin           // the idle spin loop
	wOut            // returned from drain_parallel
	// helper only
	hSleep  // waiting for a new generation
	hJoined // joined; checks phase_open
)

type worker struct {
	pc    int
	stack int
	gen   int // helper: generation seen
}

type mstate struct {
	ws          []worker
	queue       []int // full wbufs (sizes)
	wactive     int
	wnidle      int
	wdone       bool
	phaseOpen   bool
	phaseJoined int
	gen         int
	budget      int  // units that scanning may still create
	collector   int  // 0 draining, 1 set wdone, 2 helpers_wait, 3 done
	mutant      bool // leave the idle spin for queued work without wnidle--: must be caught
}

func (s *mstate) clone() *mstate {
	t := *s
	t.ws = append([]worker(nil), s.ws...)
	t.queue = append([]int(nil), s.queue...)
	return &t
}

func (s *mstate) key() string { return fmt.Sprint(*s) }

func (s *mstate) wfull() bool { return len(s.queue) > 0 }

// remaining work: units on stacks, in the queue, or being scanned
func (s *mstate) work() int {
	n := 0
	for _, w := range s.ws {
		n += w.stack
		if w.pc == wScan {
			n++
		}
	}
	for _, q := range s.queue {
		n += q
	}
	return n
}

// drainStep: one step of drain_parallel for worker i; the caller handles wOut.
func (s *mstate) drainStep(i int) []*mstate {
	w := s.ws[i]
	var out []*mstate
	switch w.pc {
	case wLoop:
		t := s.clone()
		tw := &t.ws[i]
		if tw.stack > 0 {
			tw.stack--
			tw.pc = wScan
			out = append(out, t)
			break
		}
		if len(t.queue) > 0 { // wbuf_get_full
			tw.stack += t.queue[0]
			t.queue = t.queue[1:]
			out = append(out, t)
			break
		}
		tw.pc = wIdleChk
		out = append(out, t)
	case wScan:
		// scanning pushes 0, 1 or 2 new units (within the budget); then maybe share
		for k := 0; k <= 2 && k <= s.budget; k++ {
			t := s.clone()
			t.budget -= k
			t.ws[i].stack += k
			t.ws[i].pc = wLoop
			out = append(out, t)
			if st := t.ws[i].stack; st >= 2 && t.wnidle > 0 && !t.wfull() {
				u := t.clone()
				u.queue = append(u.queue, st/2) // share
				u.ws[i].stack -= st / 2
				out = append(out, u)
			}
		}
	case wIdleChk:
		t := s.clone()
		tw := &t.ws[i]
		switch {
		case t.wactive == 1 && !t.wfull():
			tw.pc = wOut
		case t.wdone:
			tw.pc = wOut
		default:
			t.wnidle++
			if t.wnidle == t.wactive && !t.wfull() {
				t.wdone = true
				tw.pc = wOut
			} else {
				tw.pc = wSpin
			}
		}
		out = append(out, t)
	case wSpin:
		t := s.clone()
		tw := &t.ws[i]
		switch {
		case t.wdone:
			tw.pc = wOut
		case t.wfull():
			if !t.mutant {
				t.wnidle--
			}
			tw.pc = wLoop
		case t.wnidle == t.wactive && !t.wfull():
			t.wdone = true
			tw.pc = wOut
		default:
			return nil // spins (or sleeps) until something changes
		}
		out = append(out, t)
	}
	return out
}

func (s *mstate) moves() []*mstate {
	var out []*mstate
	// the collector
	switch s.collector {
	case 0:
		if s.ws[0].pc == wOut {
			t := s.clone()
			t.collector = 1
			out = append(out, t)
		} else {
			out = append(out, s.drainStep(0)...)
		}
	case 1: // wdone = 1, wake; then helpers_wait closes the phase
		t := s.clone()
		t.wdone = true
		t.phaseOpen = false
		t.collector = 2
		out = append(out, t)
	case 2:
		if s.phaseJoined == 0 {
			t := s.clone()
			t.collector = 3
			out = append(out, t)
		}
	}
	// helpers
	for i := 1; i < len(s.ws); i++ {
		w := s.ws[i]
		switch w.pc {
		case hSleep:
			if w.gen != s.gen {
				t := s.clone()
				t.ws[i].gen = t.gen
				t.phaseJoined++
				t.ws[i].pc = hJoined
				out = append(out, t)
			}
		case hJoined:
			t := s.clone()
			if t.phaseOpen {
				t.wactive++
				if !t.wdone {
					t.ws[i].pc = wLoop
				} else {
					t.ws[i].pc = wOut
				}
			} else {
				t.ws[i].pc = wOut
			}
			out = append(out, t)
		case wOut:
			t := s.clone()
			t.phaseJoined--
			t.ws[i].pc = hSleep
			out = append(out, t)
		default:
			out = append(out, s.drainStep(i)...)
		}
	}
	return out
}

func markCheck(t *testing.T, helpers, roots, budget int) int {
	n, err := markRun(helpers, roots, budget, false)
	if err != "" {
		t.Fatal(err)
	}
	return n
}

func markRun(helpers, roots, budget int, mutant bool) (int, string) {
	s0 := &mstate{wactive: 1, budget: budget, phaseOpen: true, gen: 1, mutant: mutant}
	s0.ws = append(s0.ws, worker{pc: wLoop, stack: roots})
	for i := 0; i < helpers; i++ {
		s0.ws = append(s0.ws, worker{pc: hSleep})
	}
	seen := map[string]*mstate{}
	succ := map[string][]string{}
	queue := []*mstate{s0}
	seen[s0.key()] = s0
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		k := s.key()
		if s.wdone {
			if s.work() != 0 {
				return 0, fmt.Sprintf("wdone set with %d units of work left: %s", s.work(), k)
			}
		}
		for _, n := range s.moves() {
			nk := n.key()
			succ[k] = append(succ[k], nk)
			if seen[nk] == nil {
				seen[nk] = n
				queue = append(queue, n)
			}
		}
	}
	good := map[string]bool{}
	for {
		changed := false
		for k, s := range seen {
			if good[k] {
				continue
			}
			ok := s.collector == 3
			for _, h := range s.ws[1:] {
				if h.pc != hSleep {
					ok = false
				}
			}
			for _, n := range succ[k] {
				if good[n] {
					ok = true
				}
			}
			if ok {
				good[k] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for k := range seen {
		if !good[k] {
			return 0, fmt.Sprintf("stuck: from %s the end of marking is unreachable", strings.TrimSpace(k))
		}
	}
	return len(seen), ""
}

func TestMarkMutantCaught(t *testing.T) {
	if _, err := markRun(2, 2, 3, true); err == "" {
		t.Fatal("a worker leaving the idle count without wnidle-- was not caught")
	} else {
		t.Log("caught:", err)
	}
}

func TestMark(t *testing.T) {
	for _, c := range [][3]int{{1, 2, 3}, {2, 2, 3}, {2, 4, 4}, {3, 3, 3}} {
		n := markCheck(t, c[0], c[1], c[2])
		t.Logf("%d helpers, %d roots, budget %d: %d states", c[0], c[1], c[2], n)
	}
}
