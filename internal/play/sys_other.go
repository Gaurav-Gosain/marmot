//go:build !unix

package play

import "os/exec"

// inGroup: no process groups here (wasip1 runs no processes).
func inGroup(*exec.Cmd) {}
