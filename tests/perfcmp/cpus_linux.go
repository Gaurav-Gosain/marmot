package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// cpuSet is the set of CPUs -cpus names, nil when every CPU may run the programs.
type cpuSet = *unix.CPUSet

// parseCPUs reads a CPU list like "0-3" or "0,2,4-5".
func parseCPUs(s string) (cpuSet, error) {
	if s == "" {
		return nil, nil
	}
	var set unix.CPUSet
	for part := range strings.SplitSeq(s, ",") {
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		b := a
		if err == nil && isRange {
			b, err = strconv.Atoi(hi)
		}
		if err != nil || a < 0 || b < a || b >= 1024 {
			return nil, fmt.Errorf("bad CPU list %q (like 0-3 or 0,2,4-5)", s)
		}
		for c := a; c <= b; c++ {
			set.Set(c)
		}
	}
	return &set, nil
}

// startPinned starts cmd on the CPUs of set: the fork happens on this goroutine's
// thread, locked for it, so the child inherits that thread's affinity. The thread's
// own affinity is restored after the start.
func startPinned(cmd *exec.Cmd, set cpuSet) error {
	if set == nil {
		return cmd.Start()
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var old unix.CPUSet
	if err := unix.SchedGetaffinity(0, &old); err != nil {
		return err
	}
	if err := unix.SchedSetaffinity(0, set); err != nil {
		return fmt.Errorf("pin to -cpus %s: %v", *flCPUs, err)
	}
	err := cmd.Start()
	if rerr := unix.SchedSetaffinity(0, &old); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

// cpuBusy returns the busy time of the CPUs in set (all but idle and iowait), from
// /proc/stat, or -1 without -cpus or when it cannot be read.
func cpuBusy(set cpuSet) float64 {
	if set == nil {
		return -1
	}
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return -1
	}
	total := 0.0
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 9 || !strings.HasPrefix(f[0], "cpu") || f[0] == "cpu" {
			continue
		}
		n, err := strconv.Atoi(f[0][3:])
		if err != nil || !set.IsSet(n) {
			continue
		}
		for _, i := range []int{1, 2, 3, 6, 7, 8} { // user nice system irq softirq steal
			v, _ := strconv.ParseFloat(f[i], 64)
			total += v
		}
	}
	return total / 100 // USER_HZ ticks to seconds
}
