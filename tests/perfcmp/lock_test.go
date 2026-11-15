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
