package main

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseCPUs(t *testing.T) {
	set, err := parseCPUs("0-2,5")
	if err != nil {
		t.Fatal(err)
	}
	for c, want := range []bool{true, true, true, false, false, true, false} {
		if set.IsSet(c) != want {
			t.Errorf("CPU %d in set: %v, want %v", c, set.IsSet(c), want)
		}
	}
	for _, bad := range []string{"x", "3-1", "-1", "0-", "1,,2"} {
		if _, err := parseCPUs(bad); err == nil {
			t.Errorf("parseCPUs(%q) passed", bad)
		}
	}
	if set, err := parseCPUs(""); set != nil || err != nil {
		t.Errorf("parseCPUs(\"\") = %v, %v, want nil, nil", set, err)
	}
}

func TestCPUBusy(t *testing.T) {
	if b := cpuBusy(nil); b != -1 {
		t.Errorf("cpuBusy(nil) = %v, want -1", b)
	}
	set, _ := parseCPUs("0")
	if b := cpuBusy(set); b <= 0 {
		t.Errorf("cpuBusy(CPU 0) = %v, want the busy seconds since boot", b)
	}
}

// TestStartPinned checks that the child runs on the given CPU only and that the
// calling thread gets its own affinity back.
func TestStartPinned(t *testing.T) {
	runtime.LockOSThread() // read the affinity of the thread startPinned runs on
	defer runtime.UnlockOSThread()
	var before unix.CPUSet
	if err := unix.SchedGetaffinity(0, &before); err != nil {
		t.Fatal(err)
	}
	cpu := -1
	for c := range 1024 {
		if before.IsSet(c) {
			cpu = c
			break
		}
	}
	set, err := parseCPUs(strconv.Itoa(cpu))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", "grep Cpus_allowed_list /proc/self/status")
	var out strings.Builder
	cmd.Stdout = &out
	if err := startPinned(cmd, set); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(out.String()); len(got) != 2 || got[1] != strconv.Itoa(cpu) {
		t.Errorf("child affinity %q, want CPU %d only", out.String(), cpu)
	}
	var after unix.CPUSet
	if err := unix.SchedGetaffinity(0, &after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("affinity after the start %v, want %v back", after, before)
	}
}
