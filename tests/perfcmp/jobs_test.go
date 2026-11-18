package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildJobs: build passes MARMOT_MAX_JOBS to marmot, and a side's env wins over it.
func TestBuildJobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stands in for marmot")
	}
	t.Setenv("MARMOT_MAX_JOBS", "")
	root := t.TempDir()
	bin := filepath.Join(root, "build", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// a stand-in marmot: writes its MARMOT_MAX_JOBS, then the output file (-o is $3)
	script := "#!/bin/sh\necho \"$MARMOT_MAX_JOBS\" > \"$MARMOT_ROOT/jobs\"\necho x > \"$3\"\n"
	if err := os.WriteFile(filepath.Join(bin, "marmot"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprint(min(runtime.NumCPU(), buildJobs))
	for _, c := range []struct {
		env  []string
		want string
	}{{nil, want}, {[]string{"MARMOT_MAX_JOBS=3"}, "3"}} {
		s := side{label: "A", root: root, env: c.env}
		p := program{name: "p", dir: root, pkg: "./p"}
		if _, _, err := build(s, p, "dev", t.TempDir()); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(root, "jobs"))
		if strings.TrimSpace(string(got)) != c.want {
			t.Errorf("env %v: MARMOT_MAX_JOBS=%q, want %q", c.env, strings.TrimSpace(string(got)), c.want)
		}
	}
}
