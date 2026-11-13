package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateUsage = flag.Bool("update", false, "rewrite testdata/usage.golden")

// TestUsageGolden checks the diagnostics of bad command lines (unknown
// commands and flags, missing arguments and values) at three widths, and
// once as JSON Lines: marmot's own error[M0017] format, no styled badge.
func TestUsageGolden(t *testing.T) {
	cases := [][]string{
		{"plya"},
		{"pley", "main.go"},
		{"zzz"},
		{"--foo"},
		{"--verison"},
		{"-version"},
		{"-release"},
		{"build", "-relase"},
		{"build", "-o"},
		{"size"},
		{"size", "a", "b"},
		{"size", "-n", "x", "a"},
		{"cgo"},
		{"cgo", "show"},
		{"sysroot", "ad", "linux/arm64"},
		{"explain", "M9999"},
	}
	oldOut, oldWidth := errOut, errWidth
	defer func() { errOut, errWidth = oldOut, oldWidth }()
	var out bytes.Buffer
	run1 := func(mode string, width int, args []string) {
		t.Setenv("MARMOT_OUTPUT", mode)
		var b bytes.Buffer
		errOut, errWidth = &b, width
		code := run(args)
		fmt.Fprintf(&out, "== marmot %s (%s, width %d): exit %d\n%s\n", strings.Join(args, " "), mode, width, code, b.String())
		if code != 2 {
			t.Errorf("marmot %s: exit %d, want 2", strings.Join(args, " "), code)
		}
		if strings.Contains(b.String(), "ERROR") {
			t.Errorf("marmot %s: a styled ERROR badge:\n%s", strings.Join(args, " "), b.String())
		}
	}
	for _, args := range cases {
		for _, w := range []int{60, 80, 120} {
			run1("human", w, args)
		}
	}
	for _, args := range cases[:3] {
		run1("json", 0, args)
		run1("plain", 0, args)
	}
	got := regexp.MustCompile(`"durationMs":\d+`).ReplaceAllString(out.String(), `"durationMs":0`)
	path := filepath.Join("testdata", "usage.golden")
	if *updateUsage {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestUsageGolden -update)", err)
	}
	if got != string(want) {
		t.Errorf("usage.golden differs (go test -run TestUsageGolden -update to accept)\n--- got\n%s", got)
	}
}
