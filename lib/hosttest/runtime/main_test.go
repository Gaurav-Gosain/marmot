package runtime

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain re-runs the test binary with GODEBUG=invalidptr=0: the host shim
// stores integers in pointer-typed memory (conservatively scanned memory in
// marmot), which the host collector would otherwise reject.
func TestMain(m *testing.M) {
	if !strings.Contains(os.Getenv("GODEBUG"), "invalidptr=0") {
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Env = append(os.Environ(), "GODEBUG=invalidptr=0")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				os.Exit(ee.ExitCode())
			}
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
