package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

type watchFlags struct {
	interval *time.Duration
	clear    *bool
	output   outputFlags
}

func newWatchFlags() (*flag.FlagSet, *watchFlags) {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	w := &watchFlags{}
	w.interval = fs.Duration("interval", 300*time.Millisecond, "how often to look for changed files")
	w.clear = fs.Bool("clear", true, "clear the screen before each build (terminal only)")
	w.output = addOutputFlags(fs)
	return fs, w
}

// cmdWatch implements marmot watch [build|run|test] [watch flags] [flags]
// [packages] [-- program arguments]: build (and run or test) now and again
// after every change to a .go file, go.mod or go.sum of the module. A
// running program is stopped before the next build. Each round is a marmot
// child process, so the build cache makes rebuilds incremental.
func cmdWatch(args []string) int {
	action := "build"
	if len(args) > 0 && (args[0] == "build" || args[0] == "run" || args[0] == "test") {
		action, args = args[0], args[1:]
	}
	// Watch flags come first; the rest goes to the action.
	fs, wf := newWatchFlags()
	fs.SetOutput(io.Discard)
	var rest []string
	for len(args) > 0 {
		name := strings.TrimLeft(strings.SplitN(args[0], "=", 2)[0], "-")
		if !strings.HasPrefix(args[0], "-") || fs.Lookup(name) == nil {
			break
		}
		n := 1
		if !strings.Contains(args[0], "=") && name != "clear" && name != "json" && len(args) > 1 {
			n = 2
		}
		if err := fs.Parse(args[:n]); err != nil {
			return usageError("watch", err)
		}
		args = args[n:]
	}
	rest = args
	rep := wf.output.reporter("watch")
	self, err := os.Executable()
	if err != nil {
		return rep.Usage("marmot watch: " + err.Error())
	}
	root, _ := findModuleRoot(rep.Dir)
	if root == "" {
		root = rep.Dir
	}
	tty := diag.IsTerminal(os.Stderr)
	env := os.Environ()
	if rep.Mode == diag.ModeHuman {
		// The child's stderr is ours, a terminal: keep its human output.
		env = append(env, "MARMOT_OUTPUT=human")
	}
	if *wf.output.json {
		env = append(env, "MARMOT_OUTPUT=json")
	}
	enc := json.NewEncoder(os.Stderr)
	event := func(kind string, files []string) {
		if rep.Mode == diag.ModeJSON {
			enc.Encode(map[string]any{"schema": diag.Schema, "kind": "watch", "event": kind, "files": files})
		}
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	var child *exec.Cmd
	childDone := make(chan struct{})
	stop := func() {
		if child == nil {
			return
		}
		syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
		select {
		case <-childDone:
		case <-time.After(2 * time.Second):
			syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			<-childDone
		}
		child = nil
	}
	start := func(changed []string) {
		if *wf.clear && tty && rep.Mode == diag.ModeHuman {
			fmt.Fprint(os.Stderr, "\x1b[H\x1b[2J\x1b[3J")
		}
		if rep.Mode == diag.ModeHuman {
			what := "starting"
			if len(changed) > 0 {
				what = "changed: " + strings.Join(shortList(changed, rep.Dir), ", ")
			}
			fmt.Fprintf(os.Stderr, "\x1b[2m[watch] %s, %s %s\x1b[0m\n", time.Now().Format("15:04:05"), what, action)
		}
		event("build", changed)
		child = exec.Command(self, append([]string{action}, rest...)...)
		child.Env = env
		child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "marmot watch:", err)
			child = nil
			return
		}
		done := make(chan struct{})
		childDone = done
		c := child
		began := time.Now()
		go func() {
			c.Wait()
			if rep.Mode == diag.ModeHuman {
				status := "ok"
				if c.ProcessState != nil && !c.ProcessState.Success() {
					status = fmt.Sprintf("exit %d", c.ProcessState.ExitCode())
				}
				fmt.Fprintf(os.Stderr, "\x1b[2m[watch] %s in %v; waiting for changes\x1b[0m\n", status, time.Since(began).Round(10*time.Millisecond))
			}
			event("done", nil)
			close(done)
		}()
	}

	snap := scanTree(root)
	start(nil)
	tick := time.NewTicker(*wf.interval)
	defer tick.Stop()
	for {
		select {
		case <-sigs:
			stop()
			return 0
		case <-tick.C:
			now := scanTree(root)
			changed := diffTree(snap, now)
			if len(changed) == 0 {
				continue
			}
			// Let a burst of saves settle.
			time.Sleep(50 * time.Millisecond)
			now = scanTree(root)
			changed = diffTree(snap, now)
			snap = now
			stop()
			start(changed)
		}
	}
}

type fileStamp struct {
	mod  time.Time
	size int64
}

// scanTree stamps the .go files, go.mod and go.sum under root, skipping
// version control, vendor, testdata, build output and hidden directories.
func scanTree(root string) map[string]fileStamp {
	m := map[string]fileStamp{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "vendor" || name == "testdata" || name == "node_modules" || name == "build") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum" {
			if info, err := d.Info(); err == nil {
				m[p] = fileStamp{info.ModTime(), info.Size()}
			}
		}
		return nil
	})
	return m
}

func diffTree(a, b map[string]fileStamp) []string {
	var out []string
	for p, s := range b {
		if t, ok := a[p]; !ok || t != s {
			out = append(out, p)
		}
	}
	for p := range a {
		if _, ok := b[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func shortList(files []string, dir string) []string {
	var out []string
	for i, f := range files {
		if i == 3 {
			out = append(out, fmt.Sprintf("and %d more", len(files)-3))
			break
		}
		out = append(out, diag.RelPath(dir, f))
	}
	return out
}

// findModuleRoot returns the directory of the go.mod above dir.
func findModuleRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		up := filepath.Dir(dir)
		if up == dir {
			return "", false
		}
		dir = up
	}
}
