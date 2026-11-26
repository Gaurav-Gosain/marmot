package main

import (
	"os/exec"
	"runtime"
	"testing"
)

// TestCounters: counters opened on a locked thread count a child started from it.
func TestCounters(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	c, err := openCounters()
	if err != nil {
		t.Skipf("no perf counters here: %v", err)
	}
	defer c.close()
	before, _ := c.read()
	cmd := exec.Command("sh", "-c", "i=0; while [ $i -lt 200000 ]; do i=$((i+1)); done")
	if err := startPinned(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	after, llc := c.read()
	// The loop takes some 100 ms of user time: hundreds of millions of cycles in the
	// child. This thread alone, waiting, counts far fewer.
	if after-before < 50_000_000 {
		t.Errorf("cycles %d: the child's cycles did not reach the inherited counter", after-before)
	}
	t.Logf("cycles %d, LLC misses %d", after-before, llc)
}
