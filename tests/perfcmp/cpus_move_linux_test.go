package main

import (
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// TestMoveOff runs on the real machine: after moveOff every thread of the test process
// is off the CPU and its siblings. The old mask comes back after.
func TestMoveOff(t *testing.T) {
	var all unix.CPUSet
	if err := unix.SchedGetaffinity(0, &all); err != nil {
		t.Fatal(err)
	}
	c := -1
	for i := range 1024 {
		if all.IsSet(i) {
			c = i
		}
	}
	set, _ := parseCPUs(strconv.Itoa(c))
	gone := append([]int{c}, siblings(c)...)
	defer func() {
		ents, _ := os.ReadDir("/proc/self/task")
		for _, e := range ents {
			if tid, err := strconv.Atoi(e.Name()); err == nil {
				unix.SchedSetaffinity(tid, &all)
			}
		}
	}()
	got, err := moveOff(set)
	if err != nil {
		t.Fatal(err)
	}
	if all.Count() <= len(gone) {
		if got != "" {
			t.Errorf("no CPU is left, but moveOff kept %q", got)
		}
		return
	}
	ents, _ := os.ReadDir("/proc/self/task")
	for _, e := range ents {
		tid, _ := strconv.Atoi(e.Name())
		var m unix.CPUSet
		if err := unix.SchedGetaffinity(tid, &m); err != nil {
			continue
		}
		for _, g := range gone {
			if m.IsSet(g) {
				t.Errorf("thread %d may still run on cpu %d (moveOff kept %s)", tid, g, got)
			}
		}
	}
}
