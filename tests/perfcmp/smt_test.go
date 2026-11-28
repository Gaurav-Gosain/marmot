package main

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestSlowCycles(t *testing.T) {
	for _, c := range []struct {
		name  string
		c     []uint64
		want  int
		least uint64
	}{
		{"no counters", []uint64{0, 0, 0}, -1, 0},
		{"quiet", []uint64{58e9, 59e9, 60e9, 58.5e9}, -1, 58e9},
		{"one 12% slow run", []uint64{58e9, 65e9, 58.5e9, 59e9}, 1, 58e9},
		{"exactly 8%", []uint64{100e9, 108e9}, 1, 100e9},
		{"just under 8%", []uint64{100e9, 107.9e9}, -1, 100e9},
		{"short program under the floor", []uint64{1e6, 9e8}, -1, 1e6},
	} {
		var ss []sample
		for _, n := range c.c {
			ss = append(ss, sample{cycles: n})
		}
		if j, lo := slowCycles(ss); j != c.want || lo != c.least {
			t.Errorf("%s: got run %d least %d, want %d least %d", c.name, j, lo, c.want, c.least)
		}
	}
}

func TestCPUTicks(t *testing.T) {
	dir := t.TempDir()
	defer func(r string) { procRoot = r }(procRoot)
	procRoot = dir
	stat := "cpu  9 9 9 9 9 9 9 9 0 0\n" +
		"cpu0 100 1 10 5000 7 2 3 0 0 0\n" + // busy 100+1+10+2+3+0 = 116; idle and iowait left out
		"cpu2 50 0 0 100 0 0 0 4 0 0\n" + // 54
		"intr 1 2 3\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	got := cpuTicks()
	if len(got) != 3 || got[0] != 116 || got[1] != 0 || got[2] != 54 {
		t.Errorf("got %v, want [116 0 54]", got)
	}
	if d := ticksSince([]uint64{100, 5}, []uint64{150, 4, 9}); len(d) != 3 || d[0] != 50 || d[1] != 0 || d[2] != 9 {
		t.Errorf("ticksSince got %v, want [50 0 9]", d)
	}
	if ticksSince(nil, got) != nil {
		t.Error("ticksSince with no start gave counts")
	}
}

func TestSMTLine(t *testing.T) {
	dir := t.TempDir()
	defer func(r string) { sysRoot = r }(sysRoot)
	sysRoot = dir
	for cpu, sib := range map[string]string{"cpu3": "3,11\n", "cpu11": "3,11\n", "cpu4": "4\n", "cpu5": "5-6\n"} {
		d := filepath.Join(dir, "devices/system/cpu", cpu, "topology")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "thread_siblings_list"), []byte(sib), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	busy := make([]uint64, 16)
	busy[3], busy[11], busy[5], busy[6] = 990, 450, 1000, 20
	s := sample{wall: 10 * time.Second, ran: 3, busy: busy}
	if got, want := smtLine(s), "  SMT: the run ended on cpu 3, busy 99% of the run; its sibling cpu 11 was busy 45%; other work on the core: 144% of a CPU"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	s.ran = 5
	if got, want := smtLine(s), "  SMT: the run ended on cpu 5, busy 100% of the run; its sibling cpu 6 was busy 2%; other work on the core: 102% of a CPU"; got != want {
		t.Errorf("range list: got %q, want %q", got, want)
	}
	s.ran = 4
	if got, want := smtLine(s), "  SMT: the run ended on cpu 4, busy 0% of the run; no SMT sibling found"; got != want {
		t.Errorf("one thread: got %q, want %q", got, want)
	}
	for _, u := range []sample{{wall: time.Second, ran: -1, busy: busy}, {wall: time.Second, ran: 3}, {ran: 3, busy: busy}} {
		if l := smtLine(u); l != "" {
			t.Errorf("unknown CPU, ticks or wall gave %q, want nothing", l)
		}
	}
}

// fakeSys writes thread_siblings_list files into a fake sysfs and points sysRoot at it.
func fakeSys(t *testing.T, lists map[string]string) {
	t.Helper()
	dir := t.TempDir()
	old := sysRoot
	t.Cleanup(func() { sysRoot = old })
	sysRoot = dir
	for cpu, sib := range lists {
		d := filepath.Join(dir, "devices/system/cpu", cpu, "topology")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "thread_siblings_list"), []byte(sib), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoreOther(t *testing.T) {
	fakeSys(t, map[string]string{"cpu3": "3,11\n", "cpu4": "4\n"})
	busy := make([]uint64, 16)
	s := func(ran int, b3, b11 uint64, cpu, wall time.Duration) sample {
		b := slices.Clone(busy)
		b[3], b[11] = b3, b11
		return sample{ran: ran, busy: b, cpu: cpu, wall: wall}
	}
	for _, c := range []struct {
		name string
		s    sample
		want float64
	}{
		{"quiet core", s(3, 500, 0, 5*time.Second, 5*time.Second), 0},
		{"sibling 40% busy", s(3, 500, 200, 5*time.Second, 5*time.Second), 40},
		{"time-shared on its own CPU", s(3, 1000, 0, 5*time.Second, 10*time.Second), 50},
		{"moved off the core: never above 0", s(3, 200, 0, 5*time.Second, 5*time.Second), 0},
		{"too short", s(3, 50, 50, 100*time.Millisecond, 100*time.Millisecond), -1},
		{"no SMT sibling", s(4, 500, 500, 5*time.Second, 5*time.Second), -1},
		{"no CPU", s(-1, 500, 500, 5*time.Second, 5*time.Second), -1},
		{"no ticks", sample{ran: 3, cpu: 5 * time.Second, wall: 5 * time.Second}, -1},
	} {
		if got := coreOther(c.s); math.Abs(got-c.want) > 0.01 {
			t.Errorf("%s: coreOther = %.2f, want %.2f", c.name, got, c.want)
		}
	}
	quiet, shared := s(3, 500, 0, 5*time.Second, 5*time.Second), s(3, 500, 200, 5*time.Second, 5*time.Second)
	if j, p := coreShared([]sample{quiet, quiet, shared, shared}); j != 2 || math.Abs(p-40) > 0.01 {
		t.Errorf("coreShared = %d, %.1f, want 2, 40", j, p)
	}
	at := s(3, 500, 150, 5*time.Second, 5*time.Second) // exactly 30%: not above the limit
	if j, _ := coreShared([]sample{quiet, at}); j != -1 {
		t.Errorf("a core at exactly %d%% counted as shared (run %d)", smtBusy, j)
	}
}

// TestLastCPULive runs a child on the real machine: lastCPU names a CPU of this host and
// leaves the child for Wait to reap.
func TestLastCPULive(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux only")
	}
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := lastCPU(cmd.Process.Pid)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait after lastCPU: %v", err)
	}
	if c < 0 || c >= runtime.NumCPU()*4 {
		t.Errorf("lastCPU gave %d, want a CPU of this host", c)
	}
	if sib := siblings(c); len(sib) > 0 && sib[0] == c {
		t.Errorf("siblings(%d) holds the CPU itself: %v", c, sib)
	}
}
