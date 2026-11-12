package model

// stw: the stop-the-world protocol of runtime/core (sched.c stop_the_world,
// start_the_world, sched_stop_self, rt_safepoint, rt_entersyscall, rt_exitsyscall and
// REQ_EXITSYSCALL, findrunnable, wakep, handoffp via sysmon retake; signal.c sigurg;
// gc.c gc_collect with gc_mu; lock.c rt_mutex), as a transition system.
//
// Abstractions. Each goroutine runs a script of actions (gc, syscall) and then exits. A
// goroutine that holds a P runs on its own M; Ms without a P are anonymous (startm can
// always find or create one). A spinning M started by wakep or handoffp is an entity
// "spinner" holding a P. Local run queues are empty (scripts do not create goroutines);
// the global run queue is modeled. Timeouts (notetsleep in stop_the_world, sysmon's 20us)
// are nondeterministic steps. Signals: stop_the_world's retry sends SIGURG to the M of
// every running P; the handler runs at some later step and acks only under the
// conditions of sigurg (m->locks == 0, on a goroutine stack, P running and owned).
//
// Check: from every reachable state the final state (every goroutine done, the world
// running) is still reachable. A reachable state from which it is not is a deadlock or
// a lost wake-up; the test prints the shortest trace to it.

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

const (
	pIdle = iota
	pRun
	pSys
	pStop
)

// goroutine program counters
const (
	gRun      = iota // between actions: at a safepoint
	gLockMu          // gc_collect: rt_lock(&gc_mu)
	gLockWS          // stop_the_world: rt_lock(&worldsema)
	gSTWSetup        // stop_the_world: setup under sched.lock (m->locks > 0 from here)
	gSTWWait         // stop_the_world: waiting for stopwait == 0
	gGC              // collecting
	gStart           // start_the_world
	gUnlockMu        // rt_unlock(&gc_mu)
	gSyscall         // in a syscall (P in pSys, or retaken)
	gExitSys         // rt_exitsyscall
	gQueuing         // exitsyscall decided to queue itself (mcall(REQ_EXITSYSCALL) pending)
	gQueued          // on the global run queue (needs an M with a P)
	gDone
)

type sg struct {
	pc     int
	script []byte // remaining actions: 'g' gc, 's' syscall
	p      int    // P held, -1 if none
	waits  bool   // sleeping in a lock (gc_mu or worldsema), woken by unlock
	sig    bool   // a SIGURG is pending for its M
	stop   int    // > 0: stopped in sched_stop_self, waiting for this world generation to end
	stats  bool   // the stop is rt_gc_stats (no gc_mu)
	stale  bool   // in a syscall, its P was taken: m->p still names it (only the status CAS decides)
}

type sstate struct {
	ps        []int // P status
	owner     []int // goroutine holding the P, -2 a spinner, -1 none
	gs        []sg
	gcwaiting bool
	stopwait  int
	gen       int
	mu, ws    int // owner of gc_mu, worldsema; -1 free
	runq      []int
	spinners  int  // spinners with a P (their Ps have owner -2)
	nmspin    int  // nmspinning
	before50f bool // before 3b136e2: p->m stays set during a syscall, the exit CAS does not set it
	fix       bool // 6ce0382: REQ_EXITSYSCALL takes an idle P or queues in one sched.lock
	// hold; an M that stops spinning rechecks the global queue
	spinPh int // 0 no spinner; 1 holds a P (findrunnable); 2 released its P, still spinning;
	// 3 stopped spinning (6ce0382: rechecks the global queue)
	parked []bool // goroutines parked by 'x', waiting for rt_ready from an M without a P
}

func (s *sstate) clone() *sstate {
	t := *s
	t.ps = append([]int(nil), s.ps...)
	t.owner = append([]int(nil), s.owner...)
	t.gs = make([]sg, len(s.gs))
	copy(t.gs, s.gs)
	t.runq = append([]int(nil), s.runq...)
	t.parked = append([]bool(nil), s.parked...)
	return &t
}

func (s *sstate) key() string {
	var b strings.Builder
	fmt.Fprint(&b, s.ps, s.owner, s.gcwaiting, s.stopwait, s.mu, s.ws, s.runq, s.spinners, s.nmspin, s.spinPh, s.parked, "|")
	for _, g := range s.gs {
		st := 0
		if g.stop > 0 {
			st = 1
			if g.stop != s.gen+1 {
				st = 2
			}
		}
		fmt.Fprint(&b, g.pc, string(g.script), g.p, g.waits, g.sig, st, g.stats, g.stale, ";")
	}
	return b.String()
}

func (s *sstate) idleP() int {
	for i, st := range s.ps {
		if st == pIdle && s.owner[i] == -1 {
			return i
		}
	}
	return -1
}

func (s *sstate) npidle() int {
	n := 0
	for i, st := range s.ps {
		if st == pIdle && s.owner[i] == -1 {
			n++
		}
	}
	return n
}

// locks: m->locks > 0 for these program counters (stop_the_world after its rt_lock, the
// collection, start_the_world)
func locked(pc int) bool { return pc == gSTWSetup || pc == gSTWWait || pc == gGC || pc == gStart }

// ack: sched_stop_self for goroutine i
func (s *sstate) ack(i int) bool {
	g := &s.gs[i]
	if !s.gcwaiting || g.p < 0 || s.ps[g.p] != pRun || s.owner[g.p] != i {
		return false
	}
	s.ps[g.p] = pStop
	s.stopwait--
	g.stop = s.gen + 1
	return true
}

// wakep: if an idle P and no spinning M, start a spinner on an idle P
func (s *sstate) wakep() {
	if s.npidle() == 0 || s.nmspin != 0 {
		return
	}
	s.nmspin = 1
	p := s.idleP()
	s.ps[p] = pRun
	s.owner[p] = -2
	s.spinners++
	s.spinPh = 1
}

// unlock a lock: wake one sleeping waiter (os_futex_wake of a contended word)
func (s *sstate) unlock(l *int, pcWait int) {
	*l = -1
	for i := range s.gs {
		if s.gs[i].waits && s.gs[i].pc == pcWait {
			s.gs[i].waits = false
			return
		}
	}
}

type move struct {
	label string
	s     *sstate
}

func (s *sstate) moves() []move {
	var out []move
	add := func(label string, t *sstate) { out = append(out, move{label, t}) }
	for i := range s.gs {
		g := s.gs[i]
		if g.stop > 0 {
			if g.stop != s.gen+1 { // the world started: return from sched_stop_self
				t := s.clone()
				t.gs[i].stop = 0
				add(fmt.Sprintf("g%d resumes", i), t)
			}
			continue
		}
		// a pending SIGURG is handled
		if g.sig {
			t := s.clone()
			t.gs[i].sig = false
			if !locked(g.pc) && g.pc != gSyscall && g.pc != gQueued && g.pc != gQueuing {
				t.ack(i)
			}
			add(fmt.Sprintf("g%d SIGURG", i), t)
		}
		if g.waits {
			continue
		}
		switch g.pc {
		case gRun: // a safepoint, then the next action
			if s.gcwaiting && g.p >= 0 && s.ps[g.p] == pRun {
				t := s.clone()
				t.ack(i)
				add(fmt.Sprintf("g%d safepoint ack", i), t)
				continue
			}
			t := s.clone()
			tg := &t.gs[i]
			if len(g.script) == 0 { // goroutine exits; the M runs findrunnable with its P
				tg.pc = gDone
				p := g.p
				tg.p = -1
				if len(t.runq) > 0 { // another goroutine takes over the P
					j := t.runq[0]
					t.runq = t.runq[1:]
					t.owner[p] = j
					t.gs[j].p = p
					t.gs[j].pc = gRun
				} else if t.gcwaiting {
					t.ps[p] = pStop // sched_stop_self from findrunnable, then the P stays
					t.owner[p] = -1 // (modeled as released: start_the_world idles it)
					t.stopwait--
				} else {
					t.ps[p], t.owner[p] = pIdle, -1
				}
				add(fmt.Sprintf("g%d exits", i), t)
				continue
			}
			a := g.script[0]
			tg.script = g.script[1:]
			if a == 'g' {
				tg.pc = gLockMu
				tg.stats = false
				add(fmt.Sprintf("g%d gc_collect", i), t)
			} else if a == 'x' { // park; the M runs findrunnable with the P
				p := g.p
				tg.p = -1
				tg.pc = gQueued // not runnable until readied
				t.parked[i] = true
				if len(t.runq) > 0 {
					j := t.runq[0]
					t.runq = t.runq[1:]
					t.owner[p] = j
					t.gs[j].p = p
					t.gs[j].pc = gRun
				} else if t.gcwaiting {
					t.ps[p], t.owner[p] = pStop, -1
					t.stopwait--
				} else {
					t.ps[p], t.owner[p] = pIdle, -1
				}
				add(fmt.Sprintf("g%d parks", i), t)
				continue
			} else if a == 'r' { // rt_gc_stats: stop_the_world without gc_mu
				tg.pc = gLockWS
				tg.stats = true
				add(fmt.Sprintf("g%d rt_gc_stats", i), t)
			} else { // rt_entersyscall
				t.ps[g.p] = pSys
				if t.gcwaiting {
					t.ps[g.p] = pStop
					t.owner[g.p] = -1
					t.stopwait--
					tg.p = -1
				}
				tg.pc = gSyscall
				add(fmt.Sprintf("g%d entersyscall", i), t)
			}
		case gLockMu, gLockWS:
			l := s.mu
			if g.pc == gLockWS {
				l = s.ws
			}
			t := s.clone()
			if l == -1 {
				if g.pc == gLockMu {
					t.mu = i
					t.gs[i].pc = gLockWS
				} else {
					t.ws = i
					t.gs[i].pc = gSTWSetup
				}
				add(fmt.Sprintf("g%d takes %s", i, map[bool]string{true: "gc_mu", false: "worldsema"}[g.pc == gLockMu]), t)
			} else {
				t.gs[i].waits = true
				add(fmt.Sprintf("g%d sleeps on %s", i, map[bool]string{true: "gc_mu", false: "worldsema"}[g.pc == gLockMu]), t)
			}
		case gSTWSetup:
			t := s.clone()
			n := 0
			for _, st := range t.ps {
				_ = st
				n++
			}
			t.stopwait = n
			t.gcwaiting = true
			t.ps[g.p] = pStop
			t.stopwait--
			for p := range t.ps {
				if t.ps[p] == pSys {
					t.ps[p] = pStop
					t.stopwait--
					if o := t.owner[p]; o >= 0 {
						t.gs[o].stale = true
					}
					t.owner[p] = -1
				}
			}
			for p := range t.ps {
				if t.ps[p] == pIdle && t.owner[p] == -1 {
					t.ps[p] = pStop
					t.stopwait--
				}
			}
			t.gs[i].pc = gSTWWait
			add(fmt.Sprintf("g%d stop_the_world setup (stopwait %d)", i, t.stopwait), t)
		case gSTWWait:
			if s.stopwait == 0 {
				t := s.clone()
				t.gs[i].pc = gGC
				add(fmt.Sprintf("g%d world stopped", i), t)
				continue
			}
			// notetsleep timeout: retake syscall Ps, signal running ones
			t := s.clone()
			changed := false
			for p := range t.ps {
				if t.ps[p] == pSys {
					t.ps[p] = pStop
					t.stopwait--
					if o := t.owner[p]; o >= 0 {
						t.gs[o].stale = true
					}
					t.owner[p] = -1
					changed = true
				} else if t.ps[p] == pRun && t.owner[p] >= 0 && t.owner[p] != i && !t.gs[t.owner[p]].sig {
					t.gs[t.owner[p]].sig = true
					changed = true
				}
			}
			if changed {
				add(fmt.Sprintf("g%d stop_the_world retry", i), t)
			}
		case gGC:
			t := s.clone()
			t.gs[i].pc = gStart
			add(fmt.Sprintf("g%d collects", i), t)
		case gStart:
			t := s.clone()
			for p := range t.ps {
				if t.ps[p] != pStop {
					continue
				}
				if t.owner[p] != -1 {
					t.ps[p] = pRun
				} else {
					t.ps[p] = pIdle
				}
			}
			t.gcwaiting = false
			t.gen++
			work := len(t.runq) > 0
			t.unlock(&t.ws, gLockWS)
			if work {
				t.wakep()
			}
			t.gs[i].pc = gUnlockMu
			if g.stats {
				t.gs[i].pc = gRun
			}
			add(fmt.Sprintf("g%d start_the_world (work %v)", i, work), t)
		case gUnlockMu:
			t := s.clone()
			t.unlock(&t.mu, gLockMu)
			t.gs[i].pc = gRun
			add(fmt.Sprintf("g%d unlocks gc_mu", i), t)
		case gSyscall:
			t := s.clone()
			t.gs[i].pc = gExitSys
			add(fmt.Sprintf("g%d returns from the syscall", i), t)
		case gExitSys:
			t := s.clone()
			tg := &t.gs[i]
			if g.p >= 0 && t.ps[g.p] == pSys { // CAS P_SYSCALL -> P_RUNNING on m->p
				t.ps[g.p] = pRun
				tg.pc = gRun
				if g.stale && s.before50f {
					// ABA: the P went to another M, which is now in a syscall itself;
					// this M wins the CAS but p->m still names the other M
					tg.stale = false
					add(fmt.Sprintf("g%d exitsyscall wins the CAS on P%d owned by g%d (ABA)", i, g.p, t.owner[g.p]), t)
					continue
				}
				if g.stale { // 3b136e2: the CAS winner sets p->m; the other M will lose its CAS
					if o := t.owner[g.p]; o >= 0 && o != i {
						t.gs[o].stale = true
					}
					tg.stale = false
				}
				t.owner[g.p] = i
				add(fmt.Sprintf("g%d exitsyscall keeps its P", i), t)
				continue
			}
			tg.p = -1
			tg.stale = false
			if !t.gcwaiting {
				if p := t.idleP(); p >= 0 {
					t.ps[p], t.owner[p] = pRun, i
					tg.p = p
					tg.pc = gRun
					add(fmt.Sprintf("g%d exitsyscall takes idle P%d", i, p), t)
					continue
				}
			}
			// sched.lock is dropped after the gcwaiting read; mcall(REQ_EXITSYSCALL) then
			// puts the goroutine on the global run queue in a separate step, and the M stops
			// without waking anyone.
			tg.pc = gQueuing
			add(fmt.Sprintf("g%d exitsyscall: no P (gcwaiting %v)", i, t.gcwaiting), t)
		case gQueuing:
			t := s.clone()
			if s.fix { // 6ce0382: under one sched.lock hold, an idle P or the global queue
				if p := t.idleP(); p >= 0 && !t.gcwaiting {
					t.ps[p], t.owner[p] = pRun, i
					t.gs[i].p = p
					t.gs[i].pc = gRun
					add(fmt.Sprintf("g%d REQ_EXITSYSCALL takes idle P%d", i, p), t)
					continue
				}
			}
			t.gs[i].pc = gQueued
			t.runq = append(t.runq, i)
			add(fmt.Sprintf("g%d REQ_EXITSYSCALL: on the global run queue", i), t)
		}
	}
	// sysmon: retake a P in a syscall and hand it off (handoffp)
	for p := range s.ps {
		if s.ps[p] != pSys {
			continue
		}
		t := s.clone()
		o := t.owner[p]
		t.owner[p] = -1
		if o >= 0 {
			t.gs[o].stale = true
		}
		switch {
		case len(t.runq) > 0 && t.spinPh == 0:
			t.ps[p], t.owner[p] = pRun, -2
			t.spinners++
			t.spinPh = 1
		case t.nmspin+t.npidle() == 0 && t.spinPh == 0:
			t.nmspin = 1
			t.ps[p], t.owner[p] = pRun, -2
			t.spinners++
			t.spinPh = 1
		case t.gcwaiting:
			t.ps[p] = pStop
			t.stopwait--
		default:
			t.ps[p] = pIdle
		}
		add(fmt.Sprintf("sysmon retakes P%d", p), t)
	}
	// rt_ready from an M without a P (netpoll, timers): global queue; wakep unless an M spins
	for i := range s.gs {
		if !s.parked[i] {
			continue
		}
		t := s.clone()
		t.parked[i] = false
		t.runq = append(t.runq, i)
		if t.nmspin == 0 {
			t.wakep()
		}
		add(fmt.Sprintf("rt_ready(g%d) from an M without a P", i), t)
	}
	// the spinning M (findrunnable)
	sp := -1
	for p := range s.ps {
		if s.owner[p] == -2 {
			sp = p
		}
	}
	switch s.spinPh {
	case 1:
		t := s.clone()
		switch {
		case t.gcwaiting && t.ps[sp] == pRun:
			t.ps[sp], t.owner[sp] = pStop, -1
			t.stopwait--
			t.spinners--
			t.spinPh = 2
			add(fmt.Sprintf("spinner on P%d stops for GC", sp), t)
		case len(t.runq) > 0:
			j := t.runq[0]
			t.runq = t.runq[1:]
			t.owner[sp] = j
			t.gs[j].p = sp
			t.gs[j].pc = gRun
			t.spinners--
			t.nmspin = 0
			t.spinPh = 0
			add(fmt.Sprintf("spinner on P%d runs g%d", sp, j), t)
		default: // finds nothing: releases the P (pidleput), still counted spinning
			t.ps[sp], t.owner[sp] = pIdle, -1
			t.spinners--
			t.spinPh = 2
			add(fmt.Sprintf("spinner releases P%d", sp), t)
		}
	case 2: // drop nmspinning
		t := s.clone()
		t.nmspin = 0
		t.spinPh = 3
		add("spinner stops spinning", t)
	case 3:
		t := s.clone()
		t.spinPh = 0
		if s.fix && len(t.runq) > 0 && !t.gcwaiting { // 6ce0382: recheck the global queue
			if p := t.idleP(); p >= 0 {
				j := t.runq[0]
				t.runq = t.runq[1:]
				t.ps[p], t.owner[p] = pRun, j
				t.gs[j].p = p
				t.gs[j].pc = gRun
				add(fmt.Sprintf("idle M rechecks the global queue: runs g%d on P%d", j, p), t)
				break
			}
		}
		add("idle M sleeps", t)
	}
	return out
}

func (s *sstate) final() bool {
	for _, g := range s.gs {
		if g.pc != gDone {
			return false
		}
	}
	return !s.gcwaiting
}

// stwCheck explores all states and returns a shortest trace to a state from which the
// final state cannot be reached, or nil.
func stwCheck(s0 *sstate) ([]string, int) {
	type node struct {
		s      *sstate
		parent string
		label  string
	}
	nodes := map[string]*node{}
	succ := map[string][]string{}
	k0 := s0.key()
	nodes[k0] = &node{s: s0}
	queue := []string{k0}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		for _, m := range nodes[k].s.moves() {
			mk := m.s.key()
			succ[k] = append(succ[k], mk)
			if _, ok := nodes[mk]; !ok {
				nodes[mk] = &node{s: m.s, parent: k, label: m.label}
				queue = append(queue, mk)
			}
		}
	}
	// backward reachability from final states
	pred := map[string][]string{}
	for k, ss := range succ {
		for _, x := range ss {
			pred[x] = append(pred[x], k)
		}
	}
	good := map[string]bool{}
	var work []string
	for k, n := range nodes {
		if n.s.final() {
			good[k] = true
			work = append(work, k)
		}
	}
	for len(work) > 0 {
		k := work[len(work)-1]
		work = work[:len(work)-1]
		for _, p := range pred[k] {
			if !good[p] {
				good[p] = true
				work = append(work, p)
			}
		}
	}
	// the first bad state in BFS order has a shortest trace
	var bad []string
	for k := range nodes {
		if !good[k] {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil, len(nodes)
	}
	depth := func(k string) int {
		d := 0
		for nodes[k].parent != "" {
			k = nodes[k].parent
			d++
		}
		return d
	}
	sort.Slice(bad, func(a, b int) bool { return depth(bad[a]) < depth(bad[b]) })
	var trace []string
	for k := bad[0]; nodes[k].parent != ""; k = nodes[k].parent {
		trace = append([]string{nodes[k].label}, trace...)
	}
	trace = append(trace, "stuck in: "+bad[0])
	return trace, len(nodes)
}

func newSTW(scripts ...string) *sstate {
	s := &sstate{mu: -1, ws: -1, parked: make([]bool, len(scripts))}
	for i, sc := range scripts {
		s.ps = append(s.ps, pRun)
		s.owner = append(s.owner, i)
		s.gs = append(s.gs, sg{script: []byte(sc), p: i})
	}
	return s
}

var stwScripts = [][]string{{"g", "g"}, {"g", "s"}, {"gg", "g"}, {"g", "g", "s"}, {"gs", "sg"}, {"s", "s", "g"}, {"g", "r"}, {"r", "r", "g"}, {"gr", "rg"}}

// Before 6ce0382 (at 0a1ba4a) a goroutine that leaves a syscall while the world is stopped can
// be queued after start_the_world looked for work, and nobody runs it. This test records
// the finding: it passes while the model finds that trace.
func TestSTWExitSyscallLostWakeBefore5a287c8(t *testing.T) {
	trace, _ := stwCheck(newSTW("g", "s"))
	if trace == nil {
		t.Fatal("the lost wake-up on syscall exit is no longer found: update the model to the runtime")
	}
	t.Logf("lost wake-up:\n  %s", strings.Join(trace, "\n  "))
}

// The second window 6ce0382 closes: rt_ready from an M without a P while the only M left
// is between releasing its P and dropping nmspinning skips wakep.
func TestSTWSpinWindowBefore5a287c8(t *testing.T) {
	trace, _ := stwCheck(newSTW("x", "x"))
	if trace == nil {
		t.Fatal("the spinning window is no longer found")
	}
	t.Logf("lost wake-up:\n  %s", strings.Join(trace, "\n  "))
}

// With 6ce0382, no state is stuck.
func TestSTWFixed(t *testing.T) {
	for _, sc := range append(stwScripts, []string{"x", "x"}, []string{"x", "s"}, []string{"xs", "g"}, []string{"x", "x", "g"}) {
		s := newSTW(sc...)
		s.fix = true
		trace, n := stwCheck(s)
		t.Logf("%v: %d states", sc, n)
		if trace != nil {
			t.Errorf("%v: stuck after:\n  %s", sc, strings.Join(trace, "\n  "))
		}
	}
}

// Before 3b136e2 two Ms could own one P: the P of an M in a syscall is taken and run
// by another M that enters a syscall too; the first M returns and wins the status CAS while
// p->m still names the other M. Its SIGURG handler then refuses to stop (p->m != m) and stop
// the world waits forever (the double GC hang of marmot-2).
func TestSTWTwoOwnersBefore50f70c9(t *testing.T) {
	for _, sc := range [][]string{{"sg", "xs"}, {"sg", "xsg"}, {"sg", "g", "xs"}} {
		s := newSTW(sc...)
		s.fix = true
		s.before50f = true
		trace, n := stwCheck(s)
		t.Logf("%v: %d states", sc, n)
		if trace != nil {
			t.Logf("stuck:\n  %s", strings.Join(trace, "\n  "))
			return
		}
	}
	t.Fatal("the two-owner interleaving is not found")
}

func TestSTWTwoOwnersFixed(t *testing.T) {
	for _, sc := range [][]string{{"sg", "xs"}, {"sg", "xsg"}, {"sg", "g", "xs"}} {
		s := newSTW(sc...)
		s.fix = true
		trace, n := stwCheck(s)
		t.Logf("%v: %d states", sc, n)
		if trace != nil {
			t.Errorf("%v: stuck after:\n  %s", sc, strings.Join(trace, "\n  "))
		}
	}
}
