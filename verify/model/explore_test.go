package model

// explore: the baton protocol of synctest schedule exploration
// (lib/src/runtime/explore.go). One member at a time holds the baton and
// runs user code; at a yield the holder hands it to any member waiting for
// it (every choice is explored here); a member that parks gives it up; a
// member readied by the runtime is pending until it arrives and waits for
// the baton; the last arrival chooses when the baton is free. The OS wake
// order is free: any readied member may arrive next.
//
// Checked in every reachable state: only the holder runs, the holder is
// not parked; when the baton is free, nothing is pending and nobody waits,
// every live member is durably parked (the bubble goes idle exactly when
// all are blocked); no final state leaves a member waiting for the baton,
// and the programs (deadlock free) always finish.

import (
	"fmt"
	"testing"
)

const (
	opYield = iota
	opPark  // wait for this member's token (consumes it)
	opWake  // give member arg a token, readying it if parked
	opSpawn // start member arg
	opExit
	opSleep // park until the driver fires the timer (when the bubble is idle)
)

type xop struct{ op, arg int }

const (
	mNotStarted = iota
	mRunning    // holds the baton, at pc
	mWaiting    // parked, waiting for the baton
	mParked     // durably parked on its token
	mReadied    // readied (or started), has not arrived yet
	mDone
)

type xmember struct {
	st, pc int
	token  bool
	sleep  bool // parked on a timer
}

type xstate struct {
	ms        []xmember
	holder    int // -1 free
	waiters   []int
	pending   int
	noHandoff bool // mutant: release does not hand the baton on
}

func (s *xstate) clone() *xstate {
	t := *s
	t.ms = append([]xmember(nil), s.ms...)
	t.waiters = append([]int(nil), s.waiters...)
	return &t
}

func (s *xstate) key() string { return fmt.Sprint(s.ms, s.holder, s.waiters, s.pending) }

// handTo gives the baton to waiter w (it becomes the running holder).
func (s *xstate) handTo(w int) {
	s.waiters = remove(s.waiters, w)
	s.holder = w
	s.ms[w].st = mRunning
}

// succ lists the successor states.
func (s *xstate) succ(progs [][]xop) []*xstate {
	var out []*xstate
	// The driver: when no member runs, nothing is pending and nobody
	// waits, it fires the earliest timer (any sleeper here), readying the
	// sleeper from outside the baton.
	if s.holder == -1 && s.pending == 0 && len(s.waiters) == 0 {
		for i, m := range s.ms {
			if m.st == mParked && m.sleep {
				t := s.clone()
				t.ms[i].st = mReadied
				t.ms[i].sleep = false
				t.ms[i].pc++
				t.pending++
				out = append(out, t)
			}
		}
	}
	for i := range s.ms {
		m := s.ms[i]
		switch m.st {
		case mReadied:
			// exploreArrive
			t := s.clone()
			t.pending--
			if t.holder == -1 && t.pending == 0 {
				cands := append(append([]int(nil), t.waiters...), i)
				for _, pick := range cands {
					u := t.clone()
					if pick == i {
						u.holder = i
						u.ms[i].st = mRunning
					} else {
						u.handTo(pick)
						u.waiters = append(u.waiters, i)
						u.ms[i].st = mWaiting
					}
					out = append(out, u)
				}
				continue
			}
			t.waiters = append(t.waiters, i)
			t.ms[i].st = mWaiting
			out = append(out, t)
		case mRunning:
			op := progs[i][m.pc]
			switch op.op {
			case opYield:
				if s.pending > 0 {
					continue // spins until every readied member arrived
				}
				for _, pick := range append(append([]int(nil), s.waiters...), i) {
					t := s.clone()
					t.ms[i].pc++
					if pick != i {
						t.handTo(pick)
						t.waiters = append(t.waiters, i)
						t.ms[i].st = mWaiting
					}
					out = append(out, t)
				}
			case opPark, opSleep:
				t := s.clone()
				if op.op == opSleep {
					t.ms[i].sleep = true
				} else if t.ms[i].token {
					t.ms[i].token = false
					t.ms[i].pc++
					out = append(out, t)
					continue
				}
				// exploreRelease, then the durable park (a sleeper's pc
				// moves on when the driver readies it)
				t.ms[i].st = mParked
				t.holder = -1
				if !t.noHandoff && t.pending == 0 && len(t.waiters) > 0 {
					for _, pick := range t.waiters {
						u := t.clone()
						u.handTo(pick)
						out = append(out, u)
					}
					continue
				}
				out = append(out, t)
			case opWake:
				t := s.clone()
				t.ms[i].pc++
				j := op.arg
				if t.ms[j].st == mParked {
					// exploreReady then rt_ready; the token is consumed
					// on the way out of the park.
					t.ms[j].st = mReadied
					t.ms[j].pc++
					t.pending++
				} else {
					t.ms[j].token = true
				}
				out = append(out, t)
			case opSpawn:
				t := s.clone()
				t.ms[i].pc++
				t.ms[op.arg].st = mReadied
				t.pending++
				out = append(out, t)
			case opExit:
				t := s.clone()
				t.ms[i].st = mDone
				t.holder = -1
				if !t.noHandoff && t.pending == 0 && len(t.waiters) > 0 {
					for _, pick := range t.waiters {
						u := t.clone()
						u.handTo(pick)
						out = append(out, u)
					}
					continue
				}
				out = append(out, t)
			}
		}
	}
	return out
}

func (s *xstate) check() error {
	running := 0
	for i, m := range s.ms {
		if m.st == mRunning {
			running++
			if s.holder != i {
				return fmt.Errorf("member %d runs without the baton", i)
			}
		}
	}
	if running > 1 {
		return fmt.Errorf("%d members run at once", running)
	}
	if s.holder != -1 && s.ms[s.holder].st != mRunning {
		return fmt.Errorf("holder %d is not running (state %d)", s.holder, s.ms[s.holder].st)
	}
	if s.holder == -1 && s.pending == 0 && len(s.waiters) == 0 {
		for i, m := range s.ms {
			if m.st != mParked && m.st != mDone && m.st != mNotStarted {
				return fmt.Errorf("idle with member %d live (state %d)", i, m.st)
			}
		}
	}
	return nil
}

func exploreModel(t *testing.T, progs [][]xop, noHandoff bool) error {
	s := &xstate{holder: -1, noHandoff: noHandoff}
	for range progs {
		s.ms = append(s.ms, xmember{st: mNotStarted})
	}
	// The driver starts member 0 (pending until it arrives).
	s.ms[0].st = mReadied
	s.pending = 1
	seen := map[string]bool{}
	stack := []*xstate{s}
	finals := 0
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		k := s.key()
		if seen[k] {
			continue
		}
		seen[k] = true
		if err := s.check(); err != nil {
			return fmt.Errorf("%v in %s", err, k)
		}
		next := s.succ(progs)
		if len(next) == 0 {
			finals++
			for i, m := range s.ms {
				if m.st != mDone {
					return fmt.Errorf("stuck: member %d in state %d at pc %d (%s)", i, m.st, m.pc, k)
				}
			}
		}
		stack = append(stack, next...)
	}
	t.Logf("%d states, %d final", len(seen), finals)
	return nil
}

// progs: member 0 starts 1 and 2; tokens pass 0 -> 1 -> 2 -> 0 with
// yields in between. Every P has its V, so the programs cannot deadlock.
var exploreProgs = [][]xop{
	{{opSpawn, 1}, {opYield, 0}, {opSpawn, 2}, {opWake, 1}, {opYield, 0}, {opPark, 0}, {opSpawn, 3}, {opYield, 0}, {opExit, 0}},
	{{opYield, 0}, {opPark, 0}, {opYield, 0}, {opWake, 2}, {opSleep, 0}, {opYield, 0}, {opExit, 0}},
	{{opPark, 0}, {opYield, 0}, {opWake, 0}, {opYield, 0}, {opExit, 0}},
	{{opSleep, 0}, {opYield, 0}, {opSleep, 0}, {opExit, 0}},
}

func TestExploreBaton(t *testing.T) {
	if err := exploreModel(t, exploreProgs, false); err != nil {
		t.Fatal(err)
	}
}

// Without the hand-off on release a waiter can be left with the baton
// free and nothing pending: the checker must find it.
func TestExploreBatonNoHandoffFails(t *testing.T) {
	if err := exploreModel(t, exploreProgs, true); err == nil {
		t.Fatal("the variant without hand-off passed")
	} else {
		t.Logf("found: %v", err)
	}
}
