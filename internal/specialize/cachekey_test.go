package specialize

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// MARMOT_SPEC changes generated code, so it must be part of every build
// cache key: building the same program with the switch off and then on, from
// a warm cache, must run the specializer and give a different executable.
func TestSpecSwitchInCacheKey(t *testing.T) {
	if testing.Short() {
		t.Skip("builds marmot and a program")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "build", "bin", "marmot-backend")); err != nil {
		t.Skip("no build/bin/marmot-backend")
	}
	dir := t.TempDir()
	marmot := filepath.Join(dir, "marmot")
	if out, err := exec.Command("go", "build", "-o", marmot, filepath.Join(root, "cmd", "marmot")).CombinedOutput(); err != nil {
		t.Fatalf("go build marmot: %v\n%s", err, out)
	}
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(`package main

import "fmt"

func main() {
	for i := 0; i < 3; i++ {
		fmt.Printf("%d: %5.2f %q\n", i, float64(i)/3, "x")
	}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	build := func(spec, out string) (string, []byte) {
		cmd := exec.Command(marmot, "build", "-v", "-o", out, src)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "MARMOT_ROOT="+root, "MARMOT_SPEC="+spec)
		log, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("MARMOT_SPEC=%s: %v\n%s", spec, err, log)
		}
		exe, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		run, err := exec.Command(out).Output()
		if err != nil {
			t.Fatalf("MARMOT_SPEC=%s: run: %v", spec, err)
		}
		if want := "0:  0.00 \"x\"\n1:  0.33 \"x\"\n2:  0.67 \"x\"\n"; string(run) != want {
			t.Errorf("MARMOT_SPEC=%s: output %q, want %q", spec, run, want)
		}
		return string(log), exe
	}
	// Off twice (the second from a warm cache), then on, then off again.
	build("0", filepath.Join(dir, "off0"))
	_, off := build("0", filepath.Join(dir, "off1"))
	onLog, on := build("fmt", filepath.Join(dir, "on"))
	_, off2 := build("0", filepath.Join(dir, "off2"))
	if !strings.Contains(onLog, "specialize: ") {
		t.Errorf("MARMOT_SPEC=fmt: the specializer did not run:\n%s", onLog)
	}
	if bytes.Equal(off, on) {
		t.Errorf("MARMOT_SPEC=0 and =fmt gave the same executable: the switch is not in a cache key")
	}
	if !bytes.Equal(off, off2) {
		t.Logf("MARMOT_SPEC=0 rebuilt after =fmt differs from the first (not deterministic, not a key problem)")
	}
}
