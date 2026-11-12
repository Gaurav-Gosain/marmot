package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
)

// A doctorCheck is one line of marmot doctor.
type doctorCheck struct {
	Kind   string `json:"kind"` // "doctor"
	Schema int    `json:"schema"`
	Name   string `json:"check"`
	Status string `json:"status"` // ok, warn, fail
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func newDoctorFlags() (*flag.FlagSet, outputFlags) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	return fs, addOutputFlags(fs)
}

// cmdDoctor checks what marmot needs and prints what to fix.
func cmdDoctor(args []string) int {
	fs, of := newDoctorFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("doctor", err)
	}
	rep := of.reporter("doctor")
	var checks []doctorCheck
	add := func(name, status, detail, fix string) {
		checks = append(checks, doctorCheck{Kind: "doctor", Schema: diag.Schema, Name: name, Status: status, Detail: detail, Fix: fix})
	}
	run := func(name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	add("marmot", "ok", version+" ("+runtime.GOOS+"/"+runtime.GOARCH+")", "")
	root, err := driver.FindRoot()
	if err != nil {
		add("install", "fail", err.Error(), "set MARMOT_ROOT to the directory that holds build/ and lib/, or reinstall with make install")
	} else {
		add("install", "ok", root, "")
		need := []struct{ path, what string }{
			{"build/bin/marmot-backend", "the code generator"},
			{"build/lib/librtcore.a", "the runtime core"},
			{"build/lib/rtcore.bc", "the runtime core bitcode"},
			{"lib/src/runtime", "the runtime package source"},
		}
		for _, n := range need {
			p := filepath.Join(root, n.path)
			if _, err := os.Stat(p); err != nil {
				add(n.path, "fail", n.what+" is missing: "+p, "rebuild marmot (make) or reinstall (make install)")
			}
		}
		if out, err := run(filepath.Join(root, "build/bin/marmot-backend"), "-version"); err == nil {
			first, _, _ := strings.Cut(out, "\n")
			add("backend", "ok", "marmot-backend on "+strings.TrimPrefix(first, "Homebrew "), "")
		}
		for _, t := range []string{"linux-amd64", "linux-arm64", "darwin-amd64"} {
			if _, err := os.Stat(filepath.Join(root, "build/lib", t, "librtcore.a")); err != nil {
				add("target "+t, "warn", "no runtime for -target "+strings.Replace(t, "-", "/", 1), "build it with make -C runtime/core GOOS=... GOARCH=..., or skip if you do not cross compile")
			}
		}
	}

	// The go command: marmot uses it to list and load packages.
	if out, err := run("go", "version"); err != nil {
		add("go", "fail", "the go command is not on PATH", "install Go from go.dev/dl; marmot uses it to find and load packages")
	} else {
		v := regexp.MustCompile(`go(\d+)\.(\d+)`).FindStringSubmatch(out)
		if v != nil && (v[1] != "1" || atoiOr(v[2], 0) < 27) {
			add("go", "warn", out, "marmot needs go 1.27 or newer for the standard library sources it compiles")
		} else {
			add("go", "ok", out, "")
		}
	}

	// LLVM: clang links and compiles C and IR.
	clang := "/opt/homebrew/opt/llvm@22/bin/clang"
	if d := os.Getenv("LLVM_BIN"); d != "" {
		clang = filepath.Join(d, "clang")
	}
	if out, err := run(clang, "--version"); err != nil {
		add("llvm", "fail", "no clang at "+clang, "install LLVM 22 (brew install llvm@22) or set LLVM_BIN to its bin directory")
	} else {
		first, _, _ := strings.Cut(out, "\n")
		if !strings.Contains(first, "version 22") {
			add("llvm", "warn", first, "marmot is built against LLVM 22; other versions may not read its output")
		} else {
			add("llvm", "ok", first, "")
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := run("xcrun", "--find", "ld"); err != nil {
			add("xcode", "fail", "no system linker (xcrun ld)", "install the command line tools: xcode-select --install")
		} else {
			add("xcode", "ok", out, "")
		}
		if d := firstRunDelay(clang); d > 200*time.Millisecond {
			add("first run", "warn", fmt.Sprintf("a new binary takes %v to start the first time (macOS scans every new executable)", d.Round(10*time.Millisecond)),
				"add your terminal app under System Settings > Privacy & Security > Developer Tools (run: sudo spctl developer-mode enable-terminal, then enable it there); marmot run and repl start new binaries all the time")
		}
	}

	// Cache and disk.
	if root != "" {
		size, n := driver.CacheSize(root)
		cap := capMB()
		status := "ok"
		fix := ""
		if cap > 0 && size > cap<<20 {
			status, fix = "warn", "marmot cache trim"
		}
		add("cache", status, fmt.Sprintf("%.0f MB in %d files, cap %d MB (MARMOT_CACHE_MAX_MB)", float64(size)/(1<<20), n, cap), fix)
		var st syscall.Statfs_t
		if syscall.Statfs(root, &st) == nil {
			free := float64(st.Bavail) * float64(st.Bsize) / (1 << 30)
			if free < 5 {
				add("disk", "warn", fmt.Sprintf("%.1f GB free", free), "free some space, or lower MARMOT_CACHE_MAX_MB")
			} else {
				add("disk", "ok", fmt.Sprintf("%.0f GB free", free), "")
			}
		}
	}

	// How output is chosen here.
	why := "stderr is not a terminal"
	switch {
	case os.Getenv("MARMOT_OUTPUT") != "":
		why = "MARMOT_OUTPUT=" + os.Getenv("MARMOT_OUTPUT")
	case rep.Agent != "":
		why = rep.Agent + " is set (a coding agent); MARMOT_OUTPUT=human overrides"
	case rep.Mode == diag.ModeHuman:
		why = "stderr is a terminal"
	}
	add("output", "ok", fmt.Sprintf("%s (%s)", map[diag.Mode]string{diag.ModePlain: "plain", diag.ModeHuman: "human", diag.ModeJSON: "json"}[rep.Mode], why), "")
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "MARMOT_") && k != "MARMOT_OUTPUT" {
			env = append(env, kv)
		}
	}
	if len(env) > 0 {
		add("environment", "ok", strings.Join(env, " "), "")
	}

	failed := 0
	if rep.Mode == diag.ModeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		for _, c := range checks {
			enc.Encode(c)
			if c.Status == "fail" {
				failed++
			}
		}
		diag.WriteResult(os.Stdout, diag.Result{Command: "doctor", OK: failed == 0, Errors: failed})
	} else {
		st := diag.Plain
		if diag.IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" {
			st = diag.Colors
		}
		for _, c := range checks {
			color := map[string]string{"ok": st.Help, "warn": st.Warning, "fail": st.Error}[c.Status]
			fmt.Printf("%s%-4s%s %-12s %s\n", color, c.Status, st.Reset, c.Name, c.Detail)
			if c.Fix != "" {
				fmt.Printf("     %s%-12s fix: %s%s\n", st.Dim, "", c.Fix, st.Reset)
			}
			if c.Status == "fail" {
				failed++
			}
		}
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func atoiOr(s string, d int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return d
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// firstRunDelay builds a tiny new executable and times its first start.
func firstRunDelay(clang string) time.Duration {
	dir, err := os.MkdirTemp("", "marmot-doctor-")
	if err != nil {
		return 0
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "t.c")
	// A unique constant makes a new code signature, so nothing is cached.
	os.WriteFile(src, []byte(fmt.Sprintf("int main(void){return %d %% 1;}\n", time.Now().UnixNano()%1000003)), 0o644)
	exe := filepath.Join(dir, "t")
	if err := exec.Command(clang, "-o", exe, src).Run(); err != nil {
		return 0
	}
	start := time.Now()
	if err := exec.Command(exe).Run(); err != nil {
		return 0
	}
	return time.Since(start)
}
