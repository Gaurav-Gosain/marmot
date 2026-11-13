//go:build !marmot

package main

import (
	"os"
	"runtime/pprof"
)

// startProfile writes a CPU profile to $MARMOT_CPUPROFILE and, when the
// returned function runs, an allocation profile to $MARMOT_MEMPROFILE.
func startProfile() (stop func()) {
	cpu := os.Getenv("MARMOT_CPUPROFILE")
	mem := os.Getenv("MARMOT_MEMPROFILE")
	if cpu != "" {
		if f, err := os.Create(cpu); err == nil {
			pprof.StartCPUProfile(f)
		} else {
			cpu = ""
		}
	}
	return func() {
		if cpu != "" {
			pprof.StopCPUProfile()
		}
		if mem != "" {
			if f, err := os.Create(mem); err == nil {
				pprof.Lookup("allocs").WriteTo(f, 0)
				f.Close()
			}
		}
	}
}
