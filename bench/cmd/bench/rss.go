package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Peak RSS on Linux. Go starts a child with CLONE_VM, so the child runs in the
// parent's address space until it execs, and at exec the kernel copies the high
// water mark of that address space into the child's maxrss. A child of this
// program therefore reports at least this program's own RSS (12 to 14 MB), and
// small programs all read the same. rsswrap is a small C program that forks the
// benchmark program itself and writes the program's maxrss to fd 3: the floor
// is then the RSS of rsswrap, a few hundred kilobytes. On macOS maxrss has no
// such floor and the timed runs give the RSS.
const rsswrapC = `#include <signal.h>
#include <stdio.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>

int main(int argc, char **argv) {
	if (argc < 2)
		return 2;
	pid_t pid = fork();
	if (pid < 0)
		return 127;
	if (pid == 0) {
		close(3);
		execv(argv[1], argv + 1);
		_exit(127);
	}
	int st;
	struct rusage ru;
	while (wait4(pid, &st, 0, &ru) < 0) {
	}
	dprintf(3, "%ld\n", ru.ru_maxrss);
	if (WIFSIGNALED(st)) {
		signal(WTERMSIG(st), SIG_DFL);
		raise(WTERMSIG(st));
		return 128 + WTERMSIG(st);
	}
	return WEXITSTATUS(st);
}
`

// rssRuns is how many untimed runs measure the peak RSS through rsswrap.
const rssRuns = 3

// rssHelper is the path of the compiled rsswrap, or "" when the timed runs give the RSS.
var rssHelper string

// setupRSS compiles rsswrap into dir on Linux. Without a C compiler the RSS comes from
// the timed runs, with the floor, and the results file says so.
func setupRSS(dir string) {
	if runtime.GOOS != "linux" {
		return
	}
	src, bin := filepath.Join(dir, "rsswrap.c"), filepath.Join(dir, "rsswrap")
	check(os.WriteFile(src, []byte(rsswrapC), 0o644))
	// Static first: no dynamic loader pages in the floor.
	for _, args := range [][]string{{"-O2", "-static", "-o", bin, src}, {"-O2", "-o", bin, src}} {
		if exec.Command("cc", args...).Run() == nil {
			rssHelper = bin
			return
		}
	}
	fmt.Fprintln(os.Stderr, "bench: cc cannot build rsswrap: peak RSS on Linux comes from the timed runs and has a floor of the bench process RSS")
}

// rssMethod describes how the results file measured peak RSS.
func rssMethod() string {
	switch {
	case runtime.GOOS != "linux":
		return "rusage maxrss of the timed runs, median"
	case rssHelper != "":
		return fmt.Sprintf("rusage maxrss through rsswrap (fork from a small helper), median of %d untimed runs", rssRuns)
	}
	return "rusage maxrss of the timed runs, median; floor: the bench process RSS (no cc for rsswrap)"
}

// peakRSS runs bin through rsswrap rssRuns times and returns the median peak RSS in bytes.
func peakRSS(bin string, env []string, timeout time.Duration) (int64, error) {
	var v []int64
	for range rssRuns {
		r, w, err := os.Pipe()
		if err != nil {
			return 0, err
		}
		c := exec.Command(rssHelper, bin)
		c.Dir, c.Env = root, env
		c.ExtraFiles = []*os.File{w}
		out, err := runGroup(c, timeout)
		w.Close()
		b := make([]byte, 64)
		n, _ := r.Read(b)
		r.Close()
		if err != nil {
			return 0, fmt.Errorf("rsswrap: %v: %s", err, trunc(string(out)))
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(string(b[:n])), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("rsswrap wrote %q", b[:n])
		}
		v = append(v, kb*1024)
	}
	return medianInt(v), nil
}

// rssOnlyRow finishes a -rss-only row: the output check, then the peak RSS (rsswrap on
// Linux, the median of rssRuns untimed runs elsewhere).
func rssOnlyRow(r Result, out string) Result {
	r.BinSHA256 = fileSHA(out)
	got, err := runOnce(out)
	if err != nil {
		r.Status, r.Error = "run_failed", trunc(err.Error()+"\n"+got)
		return r
	}
	want, _ := os.ReadFile(filepath.Join(root, "bench", "expected", r.Program+".txt"))
	m := string(want) == got
	r.OutputMatch = &m
	if !m {
		r.Status, r.Error = "mismatch", trunc("got:\n"+got+"\nwant:\n"+string(want))
		return r
	}
	timeout := time.Duration(*timeoutSec) * time.Second
	if rssHelper != "" {
		r.RSS, err = peakRSS(out, os.Environ(), timeout)
	} else {
		var rss []int64
		_, rss, err = timeRuns(func() *exec.Cmd { return exec.Command(out) }, "", rssRuns, 0, os.Environ(), timeout, false)
		r.RSS = medianInt(rss)
	}
	if err != nil {
		r.Status, r.Error = "run_failed", trunc(err.Error())
		return r
	}
	r.Status = "ok"
	return r
}
