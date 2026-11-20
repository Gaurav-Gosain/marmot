package main

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestPeakRSS: through rsswrap a small program reads its own peak RSS, below the
// test process's, and a program that touches 64 MB reads at least that.
func TestPeakRSS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("rsswrap is for Linux")
	}
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no cc")
	}
	dir := t.TempDir()
	defer func() { rssHelper = "" }()
	setupRSS(dir)
	if rssHelper == "" {
		t.Fatal("setupRSS built no helper")
	}
	// The test binary itself as the program: -test.run=NONE exits at once; with
	// PERFCMP_RSS_TOUCH it touches 64 MB first (see TestMain).
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := program{name: "self", dir: dir, args: []string{"-test.run=NONE"}}
	// Raise this process's peak to about 96 MB first: with the floor back, the small
	// program would read that much.
	ballast := make([]byte, 96<<20)
	for i := 0; i < len(ballast); i += 4096 {
		ballast[i] = 1
	}
	small, err := peakRSS(side{label: "A"}, p, self)
	if err != nil {
		t.Fatal(err)
	}
	big, err := peakRSS(side{label: "B", env: []string{"PERFCMP_RSS_TOUCH=1"}}, p, self)
	if err != nil {
		t.Fatal(err)
	}
	var own int64
	if b, err := os.ReadFile("/proc/self/status"); err == nil {
		own = vmHWM(string(b))
	}
	t.Logf("small %d KB, big %d KB, this process %d KB", small>>10, big>>10, own>>10)
	if big < small+60<<20 {
		t.Errorf("a program touching 64 MB read %d KB, the small one %d KB", big>>10, small>>10)
	}
	runtime.KeepAlive(ballast)
	if own > 0 && small >= own/2 {
		t.Errorf("the small program read %d KB, not below half this process's %d KB: the floor is back", small>>10, own>>10)
	}
}

// TestMain: with PERFCMP_RSS_TOUCH set the test binary is TestPeakRSS's big program.
func TestMain(m *testing.M) {
	if os.Getenv("PERFCMP_RSS_TOUCH") != "" {
		b := make([]byte, 64<<20)
		for i := 0; i < len(b); i += 4096 {
			b[i] = 1
		}
		os.Exit(int(b[len(b)-4096]) - 1)
	}
	os.Exit(m.Run())
}

// vmHWM is the VmHWM line of a /proc status file, in bytes.
func vmHWM(status string) int64 {
	for _, l := range strings.Split(status, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "VmHWM:" {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			return n << 10
		}
	}
	return 0
}
