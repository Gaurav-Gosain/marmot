package verify

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const probe = `package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"
)

func main() {
	outside, addr := os.Args[1], os.Args[2]
	werr := os.WriteFile(outside, []byte("x"), 0o644)
	c, nerr := net.DialTimeout("tcp", addr, time.Second)
	if c != nil {
		c.Close()
	}
	xerr := exec.Command("/bin/sh", "-c", "echo x > "+outside+".2").Run()
	ierr := os.WriteFile("inside.txt", []byte("x"), 0o644)
	fmt.Println(werr != nil, nerr != nil, xerr, ierr)
}
`

// TestSandboxConfines runs a program in the confirmation sandbox that
// tries to write a file outside its directory, to connect to a local
// listener and to start a shell that writes outside: all must fail, and
// a write inside its directory must work.
func TestSandboxConfines(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	if !SandboxAvailable() {
		t.Skip("no sandbox here: confirmation never runs")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command")
	}
	src, out, work := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module probe\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(work, "probe")
	build := exec.Command(gobin, "build", "-o", bin, ".")
	build.Dir = src
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, b)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepted atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			c.Close()
		}
	}()
	outside := filepath.Join(out, "escaped.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd, err := SandboxCommand(ctx, work, 10, nil, bin, outside, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	res, err := cmd.CombinedOutput()
	t.Logf("probe: %s (err %v)", strings.TrimSpace(string(res)), err)
	for _, f := range []string{outside, outside + ".2"} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("the sandboxed run wrote %s", f)
		}
	}
	if n := accepted.Load(); n != 0 {
		t.Errorf("the sandboxed run connected to the network (%d connections)", n)
	}
	if _, err := os.Stat(filepath.Join(work, "inside.txt")); err != nil {
		t.Errorf("the sandboxed run could not write its own directory: %v (output %s)", err, res)
	}
}
