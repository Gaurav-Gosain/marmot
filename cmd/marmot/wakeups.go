package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/binsize"
	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/wakeups"
)

// profWakeups implements marmot prof -wakeups: run a marmot-built program
// with the runtime's wakeup trace on (MARMOT_TRACE=wakeups=FILE), let it
// settle, and report what woke it during the window, per cause and per
// source site. With -trace FILE it reads a trace written earlier instead
// of running (args is then the binary that wrote it).
func profWakeups(pf *profFlags, args []string) int {
	rep := pf.output.reporter("prof")
	const usage = "marmot prof -wakeups [-settle 10s] [-window 60s] [-trace FILE] PROGRAM [arguments]"
	if len(args) == 0 {
		return argError(rep, "prof", usage, args, "PROGRAM")
	}
	exe := args[0]
	if !strings.ContainsRune(exe, filepath.Separator) {
		if p, err := exec.LookPath(exe); err == nil {
			exe = p
		}
	}
	trace := *pf.trace
	if trace == "" {
		f, err := os.CreateTemp("", "marmot-wakeups-*.trace")
		if err != nil {
			return rep.Usage("marmot prof: " + err.Error())
		}
		trace = f.Name()
		f.Close()
		os.Remove(trace) // the runtime creates it
		defer os.Remove(trace)
		if code := runIdle(exe, args[1:], trace, *pf.settle+*pf.window); code != 0 {
			return code
		}
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		if os.IsNotExist(err) {
			return rep.Usage(fmt.Sprintf("marmot prof: %s wrote no wakeup trace", filepath.Base(exe)),
				"the program must be built by a marmot whose runtime has the wakeup trace (MARMOT_TRACE=wakeups)")
		}
		return rep.Usage("marmot prof: " + err.Error())
	}
	h, recs, err := wakeups.Parse(data)
	if err != nil {
		return rep.Usage("marmot prof: " + trace + ": " + err.Error())
	}
	var resolve wakeups.Resolver
	if b, err := binsize.Open(exe); err == nil && b.Source == "pcln" {
		resolve = func(off uint64) wakeups.Site {
			fn, file, line, ok := b.Locate(off)
			if !ok {
				return wakeups.Site{}
			}
			wd, _ := os.Getwd()
			return wakeups.Site{Function: fn.Name, File: diag.RelPath(wd, file), Line: line}
		}
	}
	r := wakeups.Summarize(h, recs, uint64(*pf.settle), uint64(*pf.window), resolve)
	if rep.Mode == diag.ModeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(r); err != nil {
			return 1
		}
		return 0
	}
	printWakeups(r, filepath.Base(exe), resolve != nil)
	return 0
}

// runIdle runs the program with the trace on for d, then stops it
// (SIGTERM, then SIGKILL after 2 s). Its output goes to stderr.
func runIdle(exe string, args []string, trace string, d time.Duration) int {
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "MARMOT_TRACE=wakeups="+trace)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "marmot prof:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "marmot prof: running %s for %v (pid %d)\n", filepath.Base(exe), d, cmd.Process.Pid)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		fmt.Fprintf(os.Stderr, "marmot prof: %s exited before the window ended\n", filepath.Base(exe))
		return 0
	case <-time.After(d):
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	return 0
}

func printWakeups(r *wakeups.Report, name string, lines bool) {
	fmt.Printf("%s: %.2f wakeups/s (%d in %.1f s, after %.1f s to settle)\n", name, r.PerSecond, r.Total, r.WindowSec, r.SettleSec)
	if r.Total == 0 {
		fmt.Println("nothing woke it: an idle program at its best")
		return
	}
	fmt.Println("\nby cause")
	for _, c := range r.Causes {
		fmt.Printf("  %8.2f/s  %5d  %s\n", c.PerSecond, c.Count, c.Cause)
	}
	fmt.Println("\nby site")
	for i, s := range r.Sites {
		if i == 20 {
			fmt.Printf("  ... %d more\n", len(r.Sites)-20)
			break
		}
		where := s.Site.String()
		if where == "" {
			where = "(no site: the runtime itself)"
		}
		extra := ""
		if s.PeriodNs > 0 {
			extra = fmt.Sprintf(", every %v", time.Duration(s.PeriodNs))
		}
		fmt.Printf("  %8.2f/s  %5d  %-17s %s%s\n", s.PerSecond, s.Count, s.Cause, where, extra)
	}
	if !lines {
		fmt.Println("\n(no pc table in the binary: sites are not named; build without -s)")
	}
	// The timeline, one character per second: how bursty the wakeups are.
	if len(r.PerSecondT) > 0 {
		var b strings.Builder
		for _, n := range r.PerSecondT {
			switch {
			case n == 0:
				b.WriteByte('.')
			case n < 10:
				b.WriteByte(byte('0' + n))
			default:
				b.WriteByte('+')
			}
		}
		fmt.Printf("\nper second: %s\n", b.String())
	}
}
