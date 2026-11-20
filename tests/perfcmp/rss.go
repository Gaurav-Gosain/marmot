package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// Peak RSS on Linux with -rsswrap. Go starts a child with CLONE_VM, so at exec
// the kernel copies this program's own peak RSS into the child's maxrss: small
// programs then read the same on both sides (about 8 MB). rsswrap is a small C
// program that forks the program itself and writes its maxrss to fd 3; the floor
// is then the RSS of rsswrap, a few hundred kilobytes. The same helper as
// bench/cmd/bench/rss.go: keep the two in step.
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

// rssRuns is how many untimed runs measure each side's peak RSS through rsswrap.
const rssRuns = 3

// rssHelper is the path of the compiled rsswrap, or "" when the timed runs give the RSS.
var rssHelper string

// setupRSS compiles rsswrap into dir on Linux. Without a C compiler the RSS comes from the
// timed runs, with the floor, and perfcmp says so.
func setupRSS(dir string) {
	if runtime.GOOS != "linux" {
		fmt.Println("rss: -rsswrap is for Linux; here the timed runs give the peak RSS")
		return
	}
	src, bin := filepath.Join(dir, "rsswrap.c"), filepath.Join(dir, "rsswrap")
	if err := os.WriteFile(src, []byte(rsswrapC), 0o644); err != nil {
		fmt.Printf("rss: cannot write rsswrap.c: %v; the timed runs give the peak RSS\n", err)
		return
	}
	// Static first: no dynamic loader pages in the floor.
	for _, args := range [][]string{{"-O2", "-static", "-o", bin, src}, {"-O2", "-o", bin, src}} {
		if exec.Command("cc", args...).Run() == nil {
			rssHelper = bin
			fmt.Printf("rss: rusage maxrss through rsswrap (a small helper forks each program), median of %d untimed runs a side\n", rssRuns)
			return
		}
	}
	fmt.Println("rss: cc cannot build rsswrap; the timed runs give the peak RSS, with a floor of perfcmp's own RSS")
}

// peakRSS runs bin as runOnce does, but through rsswrap, rssRuns times, and returns
// the median peak RSS in bytes.
func peakRSS(s side, p program, bin string) (int64, error) {
	args := slices.Clone(p.args)
	for i, a := range args {
		if strings.HasPrefix(a, "@") {
			args[i] = filepath.Join(filepath.Dir(bin), a[1:])
		}
	}
	var v []int64
	for range rssRuns {
		r, w, err := os.Pipe()
		if err != nil {
			return 0, err
		}
		cmd := exec.Command(rssHelper, append([]string{bin}, args...)...)
		cmd.Dir = p.dir
		cmd.Env = append(os.Environ(), s.env...)
		cmd.ExtraFiles = []*os.File{w}
		err = cmd.Run()
		w.Close()
		b := make([]byte, 64)
		n, _ := r.Read(b)
		r.Close()
		if err != nil {
			return 0, fmt.Errorf("rsswrap: %v", err)
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(string(b[:n])), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("rsswrap wrote %q", b[:n])
		}
		v = append(v, kb*1024)
	}
	slices.Sort(v)
	return v[len(v)/2], nil
}
