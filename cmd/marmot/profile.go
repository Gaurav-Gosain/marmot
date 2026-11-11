//go:build !marmot

package main

import (
	"os"
	"runtime/pprof"
)

// startProfile writes a CPU profile to $MARMOT_CPUPROFILE.
func startProfile() (stop func()) {
	p := os.Getenv("MARMOT_CPUPROFILE")
	if p == "" {
		return func() {}
	}
	f, err := os.Create(p)
	if err != nil {
		return func() {}
	}
	pprof.StartCPUProfile(f)
	return pprof.StopCPUProfile
}
