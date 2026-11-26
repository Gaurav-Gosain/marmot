//go:build !linux

package main

import "errors"

// counters: the LLC check works on Linux only.
type counters struct{}

func openCounters() (*counters, error) { return nil, errors.New("perf counters work on Linux only") }

func (c *counters) read() (cycles, llc uint64) { return 0, 0 }

func (c *counters) close() {}
