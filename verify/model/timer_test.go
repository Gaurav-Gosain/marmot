package model

// timer: the timer wake path of runtime/core (timer.c timer_add, sched.c sched_timer_added,
// the stop loop of findrunnable that elects a timer M, wakep). One M (M1) has given up its
// P and runs the stop loop; a goroutine on another P adds the first timer. A spinner started
// by wakep runs findrunnable: the timer check at the top, then the stop loop like M1.
//
// Checked: from every reachable state some M eventually runs the timers (a timer M that
// sleeps with a timeout fires at the deadline; an M woken by notewakeup checks timers).

import (
	"fmt"
	"testing"
)

const (
	// M1 and spinner stop loop
	sLock    = iota // spin_lock; next := timer_next_when; elect timer M if next != MAX
	sSleep          // notetsleep(timeout): -1 if not the timer M
	sAwake          // woken: check timers
	sFired          // ran the timers
	sForever        // asleep with no timeout and nobody will wake it (only an end state)
	// spinner only
	spTop // findrunnable top: timer check
	// adder
	aStore  // update_next: timer_next_when = when (under tlock)
	aNotify // sched_timer_added: lock; if timer_m && when < timer_until: wake; else if none: wakep
	aDone
)

type tmstate struct {
	m1, sp     int // M1 and spinner program counters (sp -1: no spinner)
	m1Timer    bool
	spTimer    bool
	timerM     int // 0 none, 1 M1, 2 spinner
	until      int // timer_until (0 means far / none)
	next       int // timer_next_when (0 means INT64_MAX)
	adder      int
	idleP      int
	nmspin     int
	fired      bool
	m1Notified bool
	spNotified bool
	noWakep    bool // mutant: no wakep when there is no timer M
}

func (s tmstate) moves() []tmstate {
	var out []tmstate
	// M1 and the spinner run the same stop loop
	step := func(pc *int, isTimer *bool, notified *bool, id int) {
		t := s
		var tp *int
		var ttimer, tnot *bool
		if id == 1 {
			tp, ttimer, tnot = &t.m1, &t.m1Timer, &t.m1Notified
		} else {
			tp, ttimer, tnot = &t.sp, &t.spTimer, &t.spNotified
		}
		switch *pc {
		case spTop: // timers_check if timer_next_when <= now (the timer is due once added)
			if s.next != 0 {
				t.fired = true
				*tp = sFired
			} else {
				*tp = sLock
			}
			out = append(out, t)
		case sLock:
			if s.next != 0 && s.timerM == 0 {
				t.timerM = id
				t.until = s.next
				*ttimer = true
			}
			*tp = sSleep
			out = append(out, t)
		case sSleep:
			if *notified { // notewakeup
				*tp = sAwake
				*tnot = false
				out = append(out, t)
			} else if *isTimer { // timeout at the deadline
				*tp = sAwake
				out = append(out, t)
			}
		case sAwake:
			if s.next != 0 {
				t.fired = true
				*tp = sFired
			} else {
				*tp = sLock // loop
			}
			out = append(out, t)
		}
	}
	step(&s.m1, &s.m1Timer, &s.m1Notified, 1)
	if s.sp >= 0 {
		step(&s.sp, &s.spTimer, &s.spNotified, 2)
	}
	switch s.adder {
	case aStore:
		t := s
		t.next = 1
		t.adder = aNotify
		out = append(out, t)
	case aNotify:
		t := s
		when := 1
		if s.timerM != 0 && (s.until == 0 || when < s.until) {
			if s.timerM == 1 {
				t.m1Notified = true
			} else {
				t.spNotified = true
			}
			t.timerM = 0
		} else if s.timerM == 0 { // wakep
			if s.idleP > 0 && s.nmspin == 0 && s.sp < 0 && !s.noWakep {
				t.idleP--
				t.nmspin = 1
				t.sp = spTop
			}
		}
		t.adder = aDone
		out = append(out, t)
	}
	return out
}

func timerCheck(m1Start int) (int, string) { return timerRun(m1Start, false) }

func timerRun(m1Start int, noWakep bool) (int, string) {
	s0 := tmstate{m1: m1Start, sp: -1, idleP: 1, adder: aStore, noWakep: noWakep}
	seen := map[tmstate]bool{}
	succ := map[tmstate][]tmstate{}
	stack := []tmstate{s0}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[s] {
			continue
		}
		seen[s] = true
		for _, n := range s.moves() {
			succ[s] = append(succ[s], n)
			stack = append(stack, n)
		}
	}
	good := map[tmstate]bool{}
	for changed := true; changed; {
		changed = false
		for s := range seen {
			if good[s] {
				continue
			}
			ok := s.fired
			for _, n := range succ[s] {
				ok = ok || good[n]
			}
			if ok {
				good[s] = true
				changed = true
			}
		}
	}
	for s := range seen {
		if !good[s] {
			return len(seen), fmt.Sprintf("the timer never fires from %+v", s)
		}
	}
	return len(seen), ""
}

func TestTimerWake(t *testing.T) {
	for _, start := range []int{sLock, sSleep} {
		n, err := timerCheck(start)
		t.Logf("M1 starts at %d: %d states", start, n)
		if err != "" {
			t.Fatal(err)
		}
	}
}

func TestTimerMutantCaught(t *testing.T) {
	if _, err := timerRun(sSleep, true); err == "" {
		t.Fatal("dropping wakep when no timer M exists lost no timer")
	} else {
		t.Log("caught:", err)
	}
}
