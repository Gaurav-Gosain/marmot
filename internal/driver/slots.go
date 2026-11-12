package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// Machine-wide build slots. Every marmot process that does
// real work (load, codegen, backend, link) first takes one of N flock
// slots in a shared directory and holds it until it exits. The kernel
// drops the lock when the process dies, so a crash or kill frees the slot.
// No-change builds return before taking a slot.
//
//	MARMOT_SLOTS      number of slots (default DefaultSlots; 0 turns slots off)
//	MARMOT_SLOTS_DIR  lock directory (default /tmp/marmot-slots)
//
// While slots are on, MARMOT_MAX_JOBS defaults to slotJobs.

// DefaultSlots is the number of slots when MARMOT_SLOTS is unset.
const DefaultSlots = 3

// slotJobs is the default parallelism per slot.
const slotJobs = 2

func slotCount() int {
	if v, err := strconv.Atoi(os.Getenv("MARMOT_SLOTS")); err == nil && v >= 0 {
		return v
	}
	return DefaultSlots
}

func slotsDir() string {
	if d := os.Getenv("MARMOT_SLOTS_DIR"); d != "" {
		return d
	}
	return "/tmp/marmot-slots"
}

var heldSlot *os.File

// acquireSlot blocks until this process holds a slot. Build releases it
// when it returns; the kernel releases it if the process dies first.
func acquireSlot(logf func(string, ...any)) error {
	n := slotCount()
	if n == 0 || heldSlot != nil {
		return nil
	}
	dir := slotsDir()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("slots: %v", err)
	}
	os.Chmod(dir, 0o777)
	start := time.Now()
	noted := false
	for {
		for i := 0; i < n; i++ {
			name := filepath.Join(dir, fmt.Sprintf("slot-%d", i))
			f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o666)
			if err != nil {
				return fmt.Errorf("slots: %v", err)
			}
			if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
				heldSlot = f
				if noted {
					logf("got build slot %d after %.1fs", i, time.Since(start).Seconds())
				}
				runtime.GOMAXPROCS(maxJobs())
				return nil
			}
			f.Close()
		}
		if !noted {
			logf("waiting for a build slot (%d slots in %s busy)", n, dir)
			noted = true
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func releaseSlot() {
	if heldSlot != nil {
		heldSlot.Close()
		heldSlot = nil
	}
}
