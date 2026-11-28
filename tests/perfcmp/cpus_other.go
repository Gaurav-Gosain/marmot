//go:build !linux

package main

import (
	"errors"
	"os/exec"
)

// cpuSet: -cpus works on Linux only.
type cpuSet = *struct{}

func parseCPUs(s string) (cpuSet, error) {
	if s == "" {
		return nil, nil
	}
	return nil, errors.New("-cpus works on Linux only")
}

func startPinned(cmd *exec.Cmd, _ cpuSet) error { return cmd.Start() }

func cpuBusy(cpuSet) float64 { return -1 }

func lastCPU(int) int { return -1 }
