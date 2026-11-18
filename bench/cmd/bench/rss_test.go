package main

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// TestPeakRSSNoFloor checks that rsswrap does not report this process's RSS for a
// small program, which the plain rusage path does on Linux.
func TestPeakRSSNoFloor(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the floor is Linux only")
	}
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no cc")
	}
	root = t.TempDir()
	setupRSS(t.TempDir())
	if rssHelper == "" {
		t.Fatal("setupRSS did not build rsswrap")
	}
	defer func() { rssHelper = "" }()
	// Grow this process so the floor is clearly above a small program.
	ballast := make([]byte, 64<<20)
	for i := range ballast {
		ballast[i] = 1
	}
	_, old, err := timeRuns(func() *exec.Cmd { return exec.Command("/bin/true") }, "", 1, 0, os.Environ(), time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := peakRSS("/bin/true", os.Environ(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("/bin/true: rusage %d KB, rsswrap %d KB", old[0]>>10, got>>10)
	if got <= 0 || got > 8<<20 {
		t.Errorf("rsswrap reports %d bytes for /bin/true, want above 0 and at most 8 MB", got)
	}
	if old[0] < 64<<20 {
		t.Logf("rusage shows no floor here (%d KB): kernel without the CLONE_VM copy", old[0]>>10)
	}
	runtime.KeepAlive(ballast)
}
