// Package diagcorpus checks marmot's diagnostics on a corpus of broken
// programs. Each case in testdata/NN_name has main.go (and, where the same
// mistake exists in Rust, main.rs) with goldens side by side:
//
//	go.txt     what go build prints
//	human.txt  what marmot prints on a terminal (color off)
//	json.txt   what marmot prints in JSON mode
//	rustc.txt  what rustc prints for main.rs
//
// go test ./tests/diag checks human.txt and json.txt against the marmot in
// build/bin (or $MARMOT_BIN). -update rewrites all goldens, including go.txt
// and rustc.txt when go and rustc are on PATH.
package diagcorpus

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tier0 dev builds skip the MLIR verifier; these tests keep it on, so a
// malformed module from the frontend fails here.
func init() {
	if os.Getenv("MARMOT_VERIFY") == "" {
		os.Setenv("MARMOT_VERIFY", "1")
	}
}

var update = flag.Bool("update", false, "rewrite the goldens")

func marmotBin(t *testing.T) (bin, root string) {
	root, _ = filepath.Abs("../..")
	bin = os.Getenv("MARMOT_BIN")
	if bin == "" {
		bin = filepath.Join(root, "build", "bin", "marmot")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no marmot at %s", bin)
	}
	return bin, root
}

var durRE = regexp.MustCompile(`"durationMs":\d+`)

// run runs a command in dir and returns its combined output with dir
// replaced by $DIR.
func run(dir string, env []string, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	cmd.Run()
	out := strings.ReplaceAll(b.String(), dir+string(filepath.Separator), "")
	out = strings.ReplaceAll(out, dir, "$DIR")
	return durRE.ReplaceAllString(out, `"durationMs":0`)
}

func TestCorpus(t *testing.T) {
	bin, root := marmotBin(t)
	cases, _ := filepath.Glob("testdata/*/main.go")
	if len(cases) < 60 {
		t.Fatalf("%d cases, want at least 60", len(cases))
	}
	env := []string{"MARMOT_ROOT=" + root, "MARMOT_DAEMON=0", "NO_COLOR=1"}
	sem := make(chan bool, 3)
	var wg sync.WaitGroup
	for _, c := range cases {
		dir, _ := filepath.Abs(filepath.Dir(c))
		name := filepath.Base(dir)
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- true
			defer func() { <-sem }()
			human := run(dir, append(env, "MARMOT_OUTPUT=human"), bin, "build", "-o", os.DevNull, ".")
			js := run(dir, append(env, "MARMOT_OUTPUT=json"), bin, "build", "-o", os.DevNull, ".")
			t.Run(name, func(t *testing.T) {
				checkSchema(t, js)
				if !strings.Contains(human, "error[") && !strings.Contains(human, "error:") {
					t.Errorf("no error reported:\n%s", human)
				}
				golden(t, filepath.Join(dir, "human.txt"), human)
				golden(t, filepath.Join(dir, "json.txt"), js)
			})
			if *update {
				if _, err := exec.LookPath("go"); err == nil {
					os.WriteFile(filepath.Join(dir, "go.txt"), []byte(run(dir, []string{"GOFLAGS=-mod=mod"}, "go", "build", "-o", os.DevNull, ".")), 0o644)
				}
				if _, err := os.Stat(filepath.Join(dir, "main.rs")); err == nil {
					if _, err := exec.LookPath("rustc"); err == nil {
						tmp, _ := os.MkdirTemp("", "diagrs")
						out := run(dir, nil, "rustc", "--edition", "2021", "--color", "never", "--out-dir", tmp, "main.rs")
						os.RemoveAll(tmp)
						os.WriteFile(filepath.Join(dir, "rustc.txt"), []byte(out), 0o644)
					}
				}
			}
		}()
	}
	wg.Wait()
}

func golden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		os.WriteFile(path, []byte(got), 0o644)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs (go test ./tests/diag -update to accept)\n--- got\n%s\n--- want\n%s", filepath.Base(path), got, want)
	}
}

// checkSchema: every line is a schema 1 object, the last one the result.
func checkSchema(t *testing.T, js string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(js), "\n")
	for i, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %d is not JSON: %v\n%s", i+1, err, l)
		}
		if m["schema"] != float64(1) {
			t.Errorf("line %d: schema %v", i+1, m["schema"])
		}
		if i == len(lines)-1 {
			if m["kind"] != "result" || m["ok"] != false {
				t.Errorf("last line: %s", l)
			}
		} else if m["kind"] != "diagnostic" {
			t.Errorf("line %d kind %v", i+1, m["kind"])
		} else if m["rendered"] == nil || m["code"] == nil {
			t.Errorf("line %d lacks rendered or code", i+1)
		}
	}
}
