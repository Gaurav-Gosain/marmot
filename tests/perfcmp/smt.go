package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Core sharing: a busy SMT sibling thread, or other work on the same core, slows a run
// and raises its cycles with no LLC sign (aa5: matmul 65 G cycles against 58 G at the
// same LLC misses). A run whose user cycles are cyclesFactor times the least of its side
// or more fires the outside-load report, with the busy time of the CPU the run ended on
// and of its sibling. cyclesFloor keeps short programs, whose cycles vary a lot, out.
const (
	cyclesFactor = 1.08
	cyclesFloor  = 1_000_000_000
)

// smtBusy and smtMinWall: a run of at least smtMinWall whose core had other work above
// smtBusy percent of a CPU shared its core, and runs again (see coreShared). Busy ticks
// count in 10 ms steps, so shorter runs give no usable figure.
const (
	smtBusy    = 30
	smtMinWall = 200 * time.Millisecond
)

var sysRoot = "/sys" // a var for the tests

// slowCycles returns the index of the sample with the most cycles when it has at least
// cyclesFloor cycles and cyclesFactor times the least of ss, or -1, and that least.
// Without perf counters every count is 0 and nothing is slow.
func slowCycles(ss []sample) (int, uint64) {
	j, lo := -1, uint64(0)
	for i, s := range ss {
		if s.cycles == 0 {
			continue
		}
		if lo == 0 || s.cycles < lo {
			lo = s.cycles
		}
		if j < 0 || s.cycles > ss[j].cycles {
			j = i
		}
	}
	if j < 0 || ss[j].cycles < cyclesFloor || float64(ss[j].cycles) < cyclesFactor*float64(lo) {
		return -1, lo
	}
	return j, lo
}

// cpuTicks returns the busy ticks (user, nice, system, irq, softirq, steal) of each CPU
// from /proc/stat, indexed by CPU number, or nil when it cannot be read.
func cpuTicks() []uint64 {
	b, err := os.ReadFile(filepath.Join(procRoot, "stat"))
	if err != nil {
		return nil
	}
	var t []uint64
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 9 || !strings.HasPrefix(f[0], "cpu") || f[0] == "cpu" {
			continue
		}
		n, err := strconv.Atoi(f[0][3:])
		if err != nil || n < 0 || n >= 1024 {
			continue
		}
		var v uint64
		for _, i := range []int{1, 2, 3, 6, 7, 8} {
			x, _ := strconv.ParseUint(f[i], 10, 64)
			v += x
		}
		for len(t) <= n {
			t = append(t, 0)
		}
		t[n] = v
	}
	return t
}

// ticksSince is the busy ticks of each CPU from a to b, nil when either is nil.
func ticksSince(a, b []uint64) []uint64 {
	if a == nil || b == nil {
		return nil
	}
	d := make([]uint64, len(b))
	for i := range b {
		if i < len(a) && b[i] >= a[i] {
			d[i] = b[i] - a[i]
		}
	}
	return d
}

// siblings returns the other SMT threads of cpu's core, from sysfs, nil when unknown
// or when the core has one thread.
func siblings(cpu int) []int {
	b, err := os.ReadFile(filepath.Join(sysRoot, "devices/system/cpu", "cpu"+strconv.Itoa(cpu), "topology/thread_siblings_list"))
	if err != nil {
		return nil
	}
	var s []int
	for part := range strings.SplitSeq(strings.TrimSpace(string(b)), ",") {
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			return nil
		}
		z := a
		if isRange {
			if z, err = strconv.Atoi(hi); err != nil {
				return nil
			}
		}
		for c := a; c <= z; c++ {
			if c != cpu {
				s = append(s, c)
			}
		}
	}
	return s
}

// coreOther is the other work on the core of run s, in percent of a CPU: the busy time of
// the CPU the run ended on and of its SMT siblings, minus the CPU time of the run, over
// its wall time. A run that moved to another core counts less of its own time there, so
// the figure can only come out low, never high. -1 when unknown (no CPU, no ticks, no
// siblings, a run under smtMinWall).
func coreOther(s sample) float64 {
	if s.ran < 0 || s.ran >= len(s.busy) || s.wall < smtMinWall {
		return -1
	}
	sib := siblings(s.ran)
	if len(sib) == 0 {
		return -1
	}
	t := float64(s.busy[s.ran])
	for _, c := range sib {
		if c < len(s.busy) {
			t += float64(s.busy[c])
		}
	}
	other := 100 * (t/clkTck - s.cpu.Seconds()) / s.wall.Seconds()
	return max(other, 0)
}

// coreShared returns the index of the first sample whose core had other work above
// smtBusy percent of a CPU, or -1, and that figure.
func coreShared(ss []sample) (int, float64) {
	for i, s := range ss {
		if p := coreOther(s); p > smtBusy {
			return i, p
		}
	}
	return -1, 0
}

// smtLine says how busy the CPU a run ended on and its SMT siblings were during the
// run, "" when that is unknown.
func smtLine(s sample) string {
	if s.ran < 0 || s.ran >= len(s.busy) || s.wall <= 0 {
		return ""
	}
	pct := func(c int) float64 { return 100 * float64(s.busy[c]) / clkTck / s.wall.Seconds() }
	l := fmt.Sprintf("  SMT: the run ended on cpu %d, busy %.0f%% of the run", s.ran, pct(s.ran))
	sib := siblings(s.ran)
	if len(sib) == 0 {
		return l + "; no SMT sibling found"
	}
	for _, c := range sib {
		if c < len(s.busy) {
			l += fmt.Sprintf("; its sibling cpu %d was busy %.0f%%", c, pct(c))
		}
	}
	if o := coreOther(s); o >= 0 {
		l += fmt.Sprintf("; other work on the core: %.0f%% of a CPU", o)
	}
	return l
}
