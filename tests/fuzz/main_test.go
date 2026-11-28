package main

import (
	"strings"
	"testing"
)

// The failure log keeps the full build output of the first failing check and
// of the shrunk program, and stays within failLogMax.
func TestFailLog(t *testing.T) {
	out := string(failLog(4252, "compile error: marmot-backend failed: exit status 1",
		"marmot build -o got p.go\nline 1 of the error\nline 2 of the error\n", "stdout differs", "shrunk output\n"))
	for _, want := range []string{"seed 4252: compile error", "line 2 of the error", "== shrunk program: stdout differs", "shrunk output"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(failLog(1, "exit 1, want 0", "x", "", "")), "shrunk program") {
		t.Error("a log without shrinking names a shrunk program")
	}
	big := string(failLog(1, "compile error", strings.Repeat("x", 3*failLogMax), "", ""))
	if len(big) != failLogMax || !strings.HasSuffix(big, "[cut: the log is longer than 64 KB]\n") {
		t.Errorf("capped log: %d bytes, tail %q", len(big), big[len(big)-40:])
	}
}

// Programs that panic the same way differ after the panic line (gc's
// [signal ...] line, frame offsets); the shrinker once drifted to such a
// pair from a compile error (seed 4251). They now compare equal.
func TestNormStderr(t *testing.T) {
	want := "start\npanic: runtime error: invalid memory address or nil pointer dereference\n[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x4b53b4]\n\ngoroutine 1 [running]:\nmain.main()\n\t/x/p.go:134 +0x174\n"
	got := "start\npanic: runtime error: invalid memory address or nil pointer dereference\n\ngoroutine 1 [running]:\nmain.main(...)\n\t/x/p.go:134 +0x12b4\n"
	if normStderr(want) != normStderr(got) {
		t.Errorf("same panic, different traceback: %q and %q", normStderr(want), normStderr(got))
	}
	if normStderr("a\npanic: x\n") == normStderr("a\npanic: y\n") {
		t.Error("different panic values compare equal")
	}
	if normStderr("only output\n") != "only output\n" {
		t.Error("stderr without a panic is changed")
	}
}
