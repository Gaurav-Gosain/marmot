package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHealLock(t *testing.T) {
	timingLock = filepath.Join(t.TempDir(), "lock")
	set := func(owner string, age time.Duration) {
		os.RemoveAll(timingLock)
		if err := os.Mkdir(timingLock, 0o755); err != nil {
			t.Fatal(err)
		}
		if owner != "" {
			os.WriteFile(filepath.Join(timingLock, "owner"), []byte(owner), 0o644)
		}
		old := time.Now().Add(-age)
		os.Chtimes(timingLock, old, old)
	}
	for _, c := range []struct {
		name, owner string
		age         time.Duration
		healed      bool
	}{
		{"dead pid", "perfcmp pid 999999\n", 0, true},
		{"live pid", fmt.Sprintf("perfcmp pid %d\n", os.Getpid()), 0, false},
		{"dead pid, start time", "perfcmp started 2026-11-18T16:34:00+04:00 pid 999999\n", 0, true},
		{"live pid, start time", lockOwner(), 0, false},
		{"other format, live", fmt.Sprintf("frontend insn counts %d\n", os.Getpid()), 0, false},
		{"no pid", "someone\n", time.Hour, false},
		{"no owner, new", "", 0, false},
		{"no owner, old", "", time.Hour, true},
	} {
		set(c.owner, c.age)
		healLock()
		_, err := os.Stat(timingLock)
		if healed := os.IsNotExist(err); healed != c.healed {
			t.Errorf("%s: healed %v, want %v", c.name, healed, c.healed)
		}
	}
}

func TestNoise(t *testing.T) {
	ms := func(ns ...int) []sample {
		var ss []sample
		for _, n := range ns {
			ss = append(ss, sample{wall: time.Duration(n) * time.Millisecond})
		}
		return ss
	}
	ev, od := halves(ms(100, 110, 101, 111, 99, 109))
	if ev.wall != 100*time.Millisecond || od.wall != 110*time.Millisecond {
		t.Fatalf("halves: %v %v", ev.wall, od.wall)
	}
	if n := noiseOf(nil); n != 1 {
		t.Errorf("noiseOf(nil) = %v", n)
	}
	// 1.10 and 1/1.10 are the same spread; scaled by 1/sqrt 2: about 1.0697.
	if n := noiseOf([]float64{1.10, 1 / 1.10}); n < 1.069 || n > 1.071 {
		t.Errorf("noiseOf = %v", n)
	}
	if s := spread(1 / 1.05); s < 1.0499 || s > 1.0501 {
		t.Errorf("spread = %v", s)
	}
}

func TestPlan(t *testing.T) {
	for _, c := range []struct {
		runs   int
		first  time.Duration
		rounds int
		keep0  bool
	}{
		{7, 100 * time.Millisecond, 8, false},
		{7, 5 * time.Second, 8, false}, // at the limit: not slow
		{7, 5*time.Second + 1, 4, true},
		{7, 40 * time.Second, 4, true},
		{35, 6 * time.Second, 4, true}, // a slow startup program too
		{3, 6 * time.Second, 3, true},  // never more rounds than -runs asks for
		{2, 6 * time.Second, 2, true},
		{1, 6 * time.Second, 2, false}, // -runs 1: one warm-up, one timed run, as before
	} {
		rounds, keep0 := plan(c.runs, c.first)
		if rounds != c.rounds || keep0 != c.keep0 {
			t.Errorf("plan(%d, %v) = %d, %v, want %d, %v", c.runs, c.first, rounds, keep0, c.rounds, c.keep0)
		}
	}
}

// TestSlowSamples: the 4 samples a side of a slow program give the noise line
// its halves, and one slow run of 4 is not the median.
func TestSlowSamples(t *testing.T) {
	s := func(ns ...int) []sample {
		var ss []sample
		for _, n := range ns {
			ss = append(ss, sample{wall: time.Duration(n) * time.Millisecond})
		}
		return ss
	}
	rounds, keep0 := plan(7, 12500*time.Millisecond)
	if !keep0 || rounds != 4 {
		t.Fatalf("plan: %d, %v", rounds, keep0)
	}
	got := s(12500, 14000, 12600, 12550) // one slow run, as matmul shows
	if len(got) < 4 {
		t.Fatal("a slow program has fewer samples than the noise line needs")
	}
	if m := median(got).wall; m != 12600*time.Millisecond {
		t.Errorf("median = %v, want 12.6s", m)
	}
	ev, od := halves(got)
	if ev.wall != 12600*time.Millisecond || od.wall != 14000*time.Millisecond {
		t.Errorf("halves: %v %v", ev.wall, od.wall)
	}
}

func TestDisturbed(t *testing.T) {
	s := func(ns ...uint64) []sample {
		var ss []sample
		for _, n := range ns {
			ss = append(ss, sample{llc: n})
		}
		return ss
	}
	for _, c := range []struct {
		name string
		ss   []sample
		i    int
		lo   uint64
	}{
		{"none", nil, -1, 0},
		{"no counters", s(0, 0, 0, 0), -1, 0},
		{"quiet matmul", s(14_000_000, 7_400_000, 9_000_000, 12_000_000), -1, 7_400_000},
		{"matmul next to other work", s(10_000_000, 669_000_000, 9_000_000, 8_000_000), 1, 8_000_000},
		{"exactly 10 times", s(2_000_000, 20_000_000), 1, 2_000_000},
		{"under 10 times", s(2_000_000, 19_999_999), -1, 2_000_000},
		{"first run disturbed", s(584_000_000, 14_000_000, 7_000_000), 0, 7_000_000},
		{"short program under the floor", s(1_000, 900_000), -1, 1_000},
	} {
		i, lo := disturbed(c.ss)
		if i != c.i || lo != c.lo {
			t.Errorf("%s: disturbed = %d, %d, want %d, %d", c.name, i, lo, c.i, c.lo)
		}
	}
}

func TestVerdict(t *testing.T) {
	quiet := outcome{maxLoad: 1, cpus: 16}
	for _, c := range []struct {
		name string
		o    func(o outcome) outcome
		code int
		has  string
	}{
		{"ok", func(o outcome) outcome { return o }, exitOK, "verdict: ok"},
		{"worse", func(o outcome) outcome { o.worse = true; return o }, exitWorse, "WORSE"},
		{"noisy", func(o outcome) outcome { o.noisy = true; o.worse = true; return o }, exitInconclusive, "would have said WORSE"},
		{"loaded", func(o outcome) outcome { o.maxLoad = 9; return o }, exitInconclusive, "load reached 9.0"},
		{"disturbed after the retries", func(o outcome) outcome { o.disturbed = []string{"matmul O2 B"}; return o }, exitInconclusive, "after 2 retries: matmul O2 B"},
		{"disturbed on A=B", func(o outcome) outcome { o.disturbed, o.same = []string{"matmul dev A"}, true; return o }, exitInconclusive, "would have said ok"},
		{"failed beats all", func(o outcome) outcome {
			o.failed = []string{"nbody release: build A failed"}
			o.worse, o.noisy, o.maxLoad, o.same = true, true, 30, true
			return o
		}, exitFailed, "nbody release: build A failed"},
		{"failed on a quiet A=B", func(o outcome) outcome {
			o.same, o.failed = true, []string{"dev: no program measured on both sides"}
			return o
		}, exitFailed, "FAILED (1 failure"},
	} {
		line, code := c.o(quiet).verdict()
		if code != c.code || !strings.Contains(line, c.has) {
			t.Errorf("%s: %d %q, want %d and %q", c.name, code, line, c.code, c.has)
		}
	}
}

func TestNotFinite(t *testing.T) {
	nan := math.NaN()
	for _, c := range []struct {
		w, c, r float64
		want    string
	}{
		{1, 1, 1, ""},
		{nan, 1, 1, "wall"},
		{1, math.Inf(1), 1, "cpu"},
		{1, 1, 0, "rss"},
		{1, 1, nan, "rss"},
	} {
		if got := notFinite(c.w, c.c, c.r); got != c.want {
			t.Errorf("notFinite(%v, %v, %v) = %q, want %q", c.w, c.c, c.r, got, c.want)
		}
	}
	// geomean of nothing is NaN: a mode with no program measured
	if !math.IsNaN(geomean(nil)) {
		t.Error("geomean(nil) is a number")
	}
}
