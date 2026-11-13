package driver

import (
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
)

// eachParallel runs f(0) to f(n-1) on up to maxJobs goroutines.
func eachParallel(n int, f func(i int)) {
	w := min(maxJobs(), n)
	var next sync.Mutex
	i := 0
	var wg sync.WaitGroup
	for range w {
		wg.Go(func() {
			for {
				next.Lock()
				j := i
				i++
				next.Unlock()
				if j >= n {
					return
				}
				f(j)
			}
		})
	}
	wg.Wait()
}

// maxJobs is the parallelism for backend compiles: every core (the
// backend bounds its memory itself), or MARMOT_MAX_JOBS, or slotJobs while
// build slots are on.
func maxJobs() int {
	n := runtime.NumCPU()
	if v, err := strconv.Atoi(os.Getenv("MARMOT_MAX_JOBS")); err == nil && v > 0 {
		n = v
	} else if slotCount() > 0 && slotJobs < n {
		n = slotJobs
	}
	return n
}

// genJobs is the parallelism for MLIR generation, which runs in this
// process: every core, unless MARMOT_MAX_JOBS or build slots limit it.
func genJobs() int {
	if v, err := strconv.Atoi(os.Getenv("MARMOT_MAX_JOBS")); err == nil && v > 0 {
		return v
	}
	if slotCount() > 0 {
		return maxJobs()
	}
	return runtime.NumCPU()
}

// startGroup puts cmd in its own process group so the whole tree can be
// killed on timeout or when marmot exits.
func startGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		killGroup(cmd.Process.Pid)
		return nil
	}
}

func killGroup(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL) // fails only when the group has exited
	}
}

type pidSet struct {
	mu sync.Mutex
	m  map[int]bool
}

func (s *pidSet) add(pid int) {
	s.mu.Lock()
	if s.m == nil {
		s.m = map[int]bool{}
	}
	s.m[pid] = true
	s.mu.Unlock()
}

func (s *pidSet) remove(pid int) {
	s.mu.Lock()
	delete(s.m, pid)
	s.mu.Unlock()
}

func (s *pidSet) killAll() {
	s.mu.Lock()
	for pid := range s.m {
		killGroup(pid)
	}
	s.mu.Unlock()
}

type dirSet struct {
	mu sync.Mutex
	m  map[string]bool
}

func (s *dirSet) add(d string) {
	s.mu.Lock()
	if s.m == nil {
		s.m = map[string]bool{}
	}
	s.m[d] = true
	s.mu.Unlock()
}

func (s *dirSet) remove(d string) {
	s.mu.Lock()
	delete(s.m, d)
	s.mu.Unlock()
}

func (s *dirSet) removeAll() {
	s.mu.Lock()
	for d := range s.m {
		_ = os.RemoveAll(d) // best effort on the way out; the sweep removes what is left
	}
	s.mu.Unlock()
}

var (
	children    pidSet
	cleanupDirs dirSet
)

// Cleanup kills running tools and removes temporary directories. Safe to
// call more than once.
func Cleanup() {
	children.killAll()
	cleanupDirs.removeAll()
}

// handleSignals kills child process groups and removes temporary
// directories on SIGINT, SIGTERM or SIGHUP, then exits.
func handleSignals() (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-ch:
			Cleanup()
			if s, ok := sig.(syscall.Signal); ok {
				os.Exit(128 + int(s))
			}
			os.Exit(1)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
