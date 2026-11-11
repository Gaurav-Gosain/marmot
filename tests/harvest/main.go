// Command harvest copies single-file "// run" programs from the Go project's own test
// directory into tests/goroot/ so the conform runner can use them.
//
// Usage: go run ./tests/harvest [-src DIR] [-out DIR]
//
// A program is kept when:
//   - its first comment line is exactly "// run" (no flags, no arguments),
//   - it is package main and a single file (no "package ignored", no multi-file directories),
//   - it builds and exits within the time limit under the stock toolchain,
//   - two runs give identical stdout, stderr and exit status,
//   - it does not touch features marmot lists as non-goals (cgo, plugin, race, reflect-heavy
//     runtime internals, DWARF, os/exec, command line arguments).
//
// The license header in each file is kept as is, because the file is copied unchanged. The Go
// license text is also written to tests/goroot/LICENSE.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// allowedImports are standard packages the corpus may import. Everything else above is skipped
// unless listed here. Imports that marmot's std tier does not cover yet are still allowed because
// the point of the harvest is to find gaps.
var allowedImports = map[string]bool{
	"errors": true, "fmt": true, "strings": true, "strconv": true, "sort": true, "math": true,
	"os": true, "bytes": true, "unicode": true, "unicode/utf8": true, "sync": true,
	"sync/atomic": true, "time": true, "runtime": true, "reflect": true, "math/bits": true,
	"slices": true, "maps": true, "cmp": true, "iter": true, "unsafe": true, "io": true,
	"bufio": true, "math/cmplx": true, "math/rand": true, "unicode/utf16": true, "context": true,
	"container/heap": true, "container/list": true, "strings/builder": true,
}

var (
	flSrc  = flag.String("src", "", "Go test directory (default $(go env GOROOT)/test)")
	flOut  = flag.String("out", "", "output directory (default tests/goroot)")
	flJ    = flag.Int("j", 4, "parallel jobs (capped at 4)")
	flMax  = flag.Duration("max", 3*time.Second, "reject programs that run longer than this")
	flKeep = flag.Bool("list", false, "only list what would be kept")
	// Programs that touch these are not useful for a compiler that does not implement them.
	rejectText = regexp.MustCompile(`os\.Args|runtime\.(Caller|Callers|FuncForPC|Stack|SetFinalizer|GC\(|ReadMemStats|NumGoroutine|LockOSThread|GOMAXPROCS|Breakpoint|KeepAlive|SetCPUProfileRate)|//go:build|//go:linkname|//go:embed|testing\.|\bcgo\b|syscall\.`)
	importRe   = regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:[a-zA-Z_.]+\s+)?"([a-zA-Z0-9_/.\-]+)"\s*$`)
)

func main() {
	flag.Parse()
	root := repoRoot()
	src := *flSrc
	if src == "" {
		goroot, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			fail(err)
		}
		src = filepath.Join(strings.TrimSpace(string(goroot)), "test")
	}
	out := *flOut
	if out == "" {
		out = filepath.Join(root, "tests", "goroot")
	}
	if _, err := os.Stat(src); err != nil {
		fail(fmt.Errorf("no Go test directory at %s: %v\nfetch the matching Go source tag into .work/ and pass -src", src, err))
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		fail(err)
	}
	var cands []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if ok, _ := candidate(filepath.Join(src, e.Name())); ok {
			cands = append(cands, e.Name())
		}
	}
	sort.Strings(cands)
	fmt.Printf("%d candidates in %s\n", len(cands), src)

	work, _ := os.MkdirTemp(filepath.Join(root, ".work"), "harvest")
	defer os.RemoveAll(work)
	if err := os.MkdirAll(out, 0o755); err != nil {
		fail(err)
	}

	var mu sync.Mutex
	var kept []string
	reasons := map[string]int{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, min(max(*flJ, 1), 4))
	for _, name := range cands {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			reason := verify(filepath.Join(src, name), filepath.Join(work, strings.TrimSuffix(name, ".go")))
			mu.Lock()
			defer mu.Unlock()
			if reason != "" {
				reasons[reason]++
				return
			}
			kept = append(kept, name)
		}()
	}
	wg.Wait()
	sort.Strings(kept)
	for r, n := range reasons {
		fmt.Printf("  rejected %4d: %s\n", n, r)
	}
	fmt.Printf("keeping %d programs\n", len(kept))
	if *flKeep {
		return
	}
	// Remove stale programs from earlier harvests, then copy.
	old, _ := filepath.Glob(filepath.Join(out, "*.go"))
	for _, f := range old {
		os.Remove(f)
		for _, ext := range []string{".out", ".err", ".exit"} {
			os.Remove(strings.TrimSuffix(f, ".go") + ext)
		}
	}
	for _, name := range kept {
		data, _ := os.ReadFile(filepath.Join(src, name))
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			fail(err)
		}
	}
	if lic, err := os.ReadFile(filepath.Join(src, "..", "LICENSE")); err == nil {
		os.WriteFile(filepath.Join(out, "LICENSE"), lic, 0o644)
	}
	fmt.Printf("wrote %s. Now run: go run ./tests/conform -update -dir goroot\n", out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "harvest:", err)
	os.Exit(1)
}

func repoRoot() string {
	d, _ := os.Getwd()
	for ; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "SPEC.md")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			fail(fmt.Errorf("run from inside the repo"))
		}
	}
}

// candidate applies the cheap text checks.
func candidate(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "unreadable"
	}
	text := string(data)
	if !hasRunHeader(text) {
		return false, "not // run"
	}
	if !strings.Contains(text, "\npackage main") {
		return false, "not package main"
	}
	if rejectText.MatchString(text) {
		return false, "unsupported feature"
	}
	for _, m := range importRe.FindAllStringSubmatch(text, -1) {
		if !allowedImports[m[1]] {
			return false, "import " + m[1]
		}
	}
	return true, ""
}

func hasRunHeader(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "// run" {
			return true
		}
		if !strings.HasPrefix(line, "//") {
			return false
		}
	}
	return false
}

type res struct {
	out, err string
	code     int
	timeout  bool
}

func run(timeout time.Duration, dir, name string, args ...string) (res, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	killGroupOnCancel(cmd)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	r := res{out: so.String(), err: se.String()}
	if ctx.Err() != nil {
		r.timeout = true
		return r, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		r.code = ee.ExitCode()
		return r, nil
	}
	return r, err
}

// verify builds and runs under stock Go twice. It returns a rejection reason or "".
func verify(path, dir string) string {
	if ok, why := candidate(path); !ok {
		return why
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "mkdir"
	}
	exe := filepath.Join(dir, "p")
	b, err := run(2*time.Minute, filepath.Dir(path), "go", "build", "-o", exe, filepath.Base(path))
	if err != nil || b.code != 0 || b.timeout {
		return "does not build with stock go"
	}
	var first res
	for i := 0; i < 2; i++ {
		start := time.Now()
		r, err := run(*flMax+2*time.Second, dir, exe)
		if err != nil || r.timeout || time.Since(start) > *flMax {
			return "too slow"
		}
		if i == 0 {
			first = r
			continue
		}
		if r != first {
			return "nondeterministic"
		}
	}
	if len(first.out) > 1<<20 {
		return "huge output"
	}
	return ""
}

// killGroupOnCancel runs cmd in its own process group and kills the whole group when the
// context ends, so children of a hung compile or test never outlive it.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
