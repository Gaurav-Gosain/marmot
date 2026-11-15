//go:build marmotexplore

// Systematic schedule exploration of testing/synctest bubbles, compiled
// only with -tags marmotexplore (explore_off.go has the no-op hooks of
// default builds). Compiled only by marmot.
//
// With MARMOT_EXPLORE set (random:SEED, or pct:SEED:DEPTH[:STEPS]), every
// bubble runs one member at a time: the member holding the baton. At a
// decision point (a channel operation, select, go statement, a sync
// Lock or Unlock) the holder asks the strategy which member runs next
// among itself and the members waiting for the baton; a member that
// blocks gives the baton up, and a member that wakes from a park, or
// starts, waits for it. Members the runtime readied but that have not
// yet asked for the baton are counted (pending); no choice is made until
// they have all arrived, so the choice depends only on the seed and the
// program, not on which thread woke first. Preemption and the number of
// threads do not matter: members without the baton are parked.
//
// A member waiting for the baton is parked but not durably blocked: the
// bubble only goes idle when the baton is free, nothing is pending and
// nobody waits for it, that is when every member is durably blocked.

package runtime

import "unsafe"

const exploreEnabled = true

const (
	exploreRandom = 1
	explorePCT    = 2
)

// exploreG is a member's state: its id, PCT priority, and whether the
// runtime readied it and it has not yet asked for the baton.
type exploreG struct {
	id      int64
	prio    int64
	readied bool
}

type explorer struct {
	mu       mutex
	holder   *g
	waiters  []*g
	pending  int32
	parked   *g // the holder, parked until the pending members arrive
	members  map[*g]*exploreG
	starts   map[*bubbleStart]*exploreG // ids given by starters
	nextID   int64
	strategy int
	seed     uint64
	rng      uint64
	steps    int64
	changes  []int64 // PCT priority change points (decision numbers)
	nchanged int64
	switches int64
	spec     string
}

var (
	exploreSpecDone bool
	exploreSpec     string
	// exploreActive is set once a bubble explores (before its members
	// start); the hooks test it first.
	exploreActive bool
	explorers     map[*bubble]*explorer
	explorersLock mutex
)

// exploreBubble starts exploring b when MARMOT_EXPLORE asks for it. It
// runs in synctest_Run before any member starts.
func exploreBubble(b *bubble) {
	x := newExplorer()
	if x == nil {
		return
	}
	lock(&explorersLock)
	if explorers == nil {
		explorers = map[*bubble]*explorer{}
	}
	explorers[b] = x
	exploreActive = true
	unlock(&explorersLock)
}

// exploreBubbleDone forgets b's explorer.
func exploreBubbleDone(b *bubble) {
	if !exploreActive {
		return
	}
	if x := explorerOf(b); x != nil && gogetenv("MARMOT_EXPLORE_DEBUG") != "" {
		print("explore: ", x.steps, " decisions, ", x.switches, " switches\n")
	}
	lock(&explorersLock)
	delete(explorers, b)
	unlock(&explorersLock)
}

func explorerOf(b *bubble) *explorer {
	if b == nil || !exploreActive {
		return nil
	}
	lock(&explorersLock)
	x := explorers[b]
	unlock(&explorersLock)
	return x
}

// newExplorer parses MARMOT_EXPLORE; nil when exploration is off.
func newExplorer() *explorer {
	if !exploreSpecDone {
		exploreSpec = gogetenv("MARMOT_EXPLORE")
		exploreSpecDone = true
	}
	s := exploreSpec
	if s == "" {
		return nil
	}
	x := &explorer{spec: s, members: map[*g]*exploreG{}, starts: map[*bubbleStart]*exploreG{}}
	f := splitColon(s)
	switch f[0] {
	case "random":
		x.strategy = exploreRandom
	case "pct":
		x.strategy = explorePCT
	default:
		throw("MARMOT_EXPLORE: strategy must be random or pct")
	}
	if len(f) > 1 {
		x.seed = uint64(atoiPlain(f[1]))
	}
	x.rng = x.seed*0x9e3779b97f4a7c15 + 1
	if x.strategy == explorePCT {
		depth, steps := int64(3), int64(30)
		if len(f) > 2 {
			depth = atoiPlain(f[2])
		}
		if len(f) > 3 {
			steps = atoiPlain(f[3])
		}
		for i := int64(1); i < depth; i++ {
			x.changes = append(x.changes, 1+int64(x.next()%uint64(steps)))
		}
	}
	return x
}

func splitColon(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func atoiPlain(s string) int64 {
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n
}

func (x *explorer) next() uint64 {
	x.rng += 0xa0761d6478bd642f
	hi, lo := mul64(x.rng, x.rng^0xe7037ed1a0b428db)
	return hi ^ lo
}

// exploring returns gp's explorer and member state, nil outside an
// explored bubble.
func exploring(gp *g) (*explorer, *exploreG) {
	x := explorerOf(gp.bubble)
	if x == nil {
		return nil, nil
	}
	lock(&x.mu)
	m := x.members[gp]
	unlock(&x.mu)
	if m == nil {
		return nil, nil
	}
	return x, m
}

// exploreNewMember gives the member s starts its id and priority, in the
// starter's (deterministic) order, and counts it pending.
func exploreNewMember(b *bubble, s *bubbleStart) {
	x := explorerOf(b)
	if x == nil {
		return
	}
	lock(&x.mu)
	x.nextID++
	x.starts[s] = &exploreG{id: x.nextID, prio: int64(x.next()>>2) + 1<<20, readied: true}
	x.pending++
	unlock(&x.mu)
}

// exploreStarted registers gp as the member s started, seeds its random
// state from the schedule, and waits for the baton.
func exploreStarted(gp *g, s *bubbleStart) {
	x := explorerOf(s.b)
	if x == nil {
		return
	}
	lock(&x.mu)
	m := x.starts[s]
	delete(x.starts, s)
	x.members[gp] = m
	unlock(&x.mu)
	if m == nil {
		return
	}
	gp.rand = x.seed ^ uint64(m.id)*0xbf58476d1ce4e5b9
	exploreArrive(gp)
}

// choose picks the member to run among cands, x.mu held.
func (x *explorer) choose(cands []*g, cur *g) *g {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && x.members[cands[j]].id < x.members[cands[j-1]].id; j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
	x.steps++
	if x.strategy == explorePCT {
		for _, c := range x.changes {
			if c == x.steps && cur != nil {
				x.nchanged++
				x.members[cur].prio = x.nchanged
			}
		}
		best := cands[0]
		for _, c := range cands[1:] {
			if x.members[c].prio > x.members[best].prio {
				best = c
			}
		}
		return best
	}
	return cands[uint32(x.next()%uint64(len(cands)))]
}

func (x *explorer) removeWaiter(gp *g) {
	for i, w := range x.waiters {
		if w == gp {
			x.waiters = append(x.waiters[:i], x.waiters[i+1:]...)
			return
		}
	}
}

// waitBaton parks the caller until it holds the baton. x.mu is held and
// released.
func (x *explorer) waitBaton(gp *g) {
	x.waiters = append(x.waiters, gp)
	for {
		rt_park(parkunlockPC, unsafe.Pointer(&x.mu))
		lock(&x.mu)
		if x.holder == gp {
			unlock(&x.mu)
			return
		}
	}
}

// handTo gives the baton to waiter w and wakes it. x.mu is held.
func (x *explorer) handTo(w *g) {
	x.removeWaiter(w)
	x.holder = w
	rt_ready(w.handle)
}

// exploreYield is a decision point of the running member.
func exploreYield(gp *g) {
	x, _ := exploring(gp)
	if x == nil {
		return
	}
	lock(&x.mu)
	if x.holder != gp {
		// Not the holder (outside the protocol): nothing to decide.
		unlock(&x.mu)
		return
	}
	// The members readied but not yet arrived take part in the choice:
	// park until the last of them wakes us.
	for x.pending > 0 {
		x.parked = gp
		rt_park(parkunlockPC, unsafe.Pointer(&x.mu))
		lock(&x.mu)
	}
	cands := append(append([]*g(nil), x.waiters...), gp)
	pick := x.choose(cands, gp)
	if pick == gp {
		unlock(&x.mu)
		return
	}
	x.switches++
	x.handTo(pick)
	x.waitBaton(gp)
}

// exploreRelease gives up the baton before the member blocks or exits.
func exploreRelease(gp *g) {
	x, _ := exploring(gp)
	if x == nil {
		return
	}
	lock(&x.mu)
	if x.holder == gp {
		x.holder = nil
		if x.pending == 0 && len(x.waiters) > 0 {
			x.handTo(x.choose(append([]*g(nil), x.waiters...), nil))
		}
	}
	unlock(&x.mu)
}

// exploreExit releases the baton and forgets the member.
func exploreExit(gp *g) {
	x, _ := exploring(gp)
	if x == nil {
		return
	}
	exploreRelease(gp)
	lock(&x.mu)
	delete(x.members, gp)
	unlock(&x.mu)
}

// exploreArrive runs when a member resumes after a park, or starts: it
// waits for the baton. The last pending member to arrive makes the
// choice when the baton is free, or wakes the holder waiting in yield.
func exploreArrive(gp *g) {
	x, m := exploring(gp)
	if x == nil {
		return
	}
	lock(&x.mu)
	if m.readied {
		m.readied = false
		x.pending--
	}
	if x.pending == 0 && x.parked != nil {
		h := x.parked
		x.parked = nil
		rt_ready(h.handle)
	}
	if x.holder == nil && x.pending == 0 {
		pick := x.choose(append(append([]*g(nil), x.waiters...), gp), nil)
		if pick == gp {
			x.holder = gp
			unlock(&x.mu)
			return
		}
		x.handTo(pick)
	}
	if x.holder == gp {
		unlock(&x.mu)
		return
	}
	x.waitBaton(gp)
}

// exploreReady counts gp as pending before the runtime readies it.
func exploreReady(gp *g) {
	x, m := exploring(gp)
	if x == nil {
		return
	}
	lock(&x.mu)
	if !m.readied {
		m.readied = true
		x.pending++
	}
	unlock(&x.mu)
}

// exploreReport is appended to bubble failures for replay.
func exploreReport(b *bubble) string {
	x := explorerOf(b)
	if x == nil {
		return ""
	}
	return " (schedule MARMOT_EXPLORE=" + x.spec + ")"
}

// The sync package's decision points (Lock, Unlock, Wait) under
// marmotexplore.
//
//go:linkname sync_runtime_exploreYield sync.runtime_exploreYield
func sync_runtime_exploreYield() {
	if exploreActive {
		exploreYield(getg())
	}
}
