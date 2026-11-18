package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A gate command that prints ready holds until perfcmp closes its stdin.
func TestTakeGate(t *testing.T) {
	d := t.TempDir()
	mark := filepath.Join(d, "released")
	g := filepath.Join(d, "gate.sh")
	os.WriteFile(g, []byte("echo ready\ncat > /dev/null\necho free\ntouch "+mark+"\n"), 0o644)
	release, err := takeGate(g)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("the gate was given back before release")
	}
	release()
	release() // a second call does nothing
	if _, err := os.Stat(mark); err != nil {
		t.Fatal("release did not end the gate command")
	}
}

// A gate command that ends or prints something else is an error, not a timing.
func TestTakeGateNotReady(t *testing.T) {
	d := t.TempDir()
	for _, body := range []string{"exit 1\n", "echo busy\n"} {
		g := filepath.Join(d, "gate.sh")
		os.WriteFile(g, []byte(body), 0o644)
		if _, err := takeGate(g); err == nil || !strings.Contains(err.Error(), "did not print ready") {
			t.Errorf("%q: got %v, want a not-ready error", body, err)
		}
	}
}
