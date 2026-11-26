package main

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// counters are user-mode cycle and LLC read-miss counters opened on the calling
// thread with inherit set: a child forked from that thread after the open is
// counted, and its counts reach these counters when it exits. The caller keeps
// the thread locked from the open until after the child's Wait, so no other
// goroutine runs on it.
type counters struct{ fd [2]int }

func openCounters() (*counters, error) {
	attrs := [2]unix.PerfEventAttr{
		{Type: unix.PERF_TYPE_HARDWARE, Config: unix.PERF_COUNT_HW_CPU_CYCLES},
		{Type: unix.PERF_TYPE_HW_CACHE, Config: unix.PERF_COUNT_HW_CACHE_LL | unix.PERF_COUNT_HW_CACHE_OP_READ<<8 | unix.PERF_COUNT_HW_CACHE_RESULT_MISS<<16},
	}
	c := &counters{fd: [2]int{-1, -1}}
	for i := range attrs {
		a := &attrs[i]
		a.Size = uint32(unsafe.Sizeof(*a))
		a.Bits = unix.PerfBitInherit | unix.PerfBitExcludeKernel | unix.PerfBitExcludeHv
		fd, err := unix.PerfEventOpen(a, 0, -1, -1, unix.PERF_FLAG_FD_CLOEXEC)
		if err != nil {
			c.close()
			return nil, fmt.Errorf("perf_event_open: %v", err)
		}
		c.fd[i] = fd
	}
	return c, nil
}

// read returns the cycles and LLC misses counted so far, 0 and 0 on a nil c.
func (c *counters) read() (cycles, llc uint64) {
	if c == nil {
		return 0, 0
	}
	var v [2]uint64
	for i, fd := range c.fd {
		var b [8]byte
		if n, err := unix.Read(fd, b[:]); err == nil && n == 8 {
			v[i] = binary.NativeEndian.Uint64(b[:])
		}
	}
	return v[0], v[1]
}

func (c *counters) close() {
	if c == nil {
		return
	}
	for _, fd := range c.fd {
		if fd >= 0 {
			unix.Close(fd)
		}
	}
}
