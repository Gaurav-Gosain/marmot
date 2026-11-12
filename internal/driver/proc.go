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

// maxJobs is the parallelism for backend compiles (at most 4).
func maxJobs() int {
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if v, err := strconv.Atoi(os.Getenv("MARMOT_MAX_JOBS")); err == nil && v > 0 {
		if v < n {
			n = v
		}
	} else if slotCount() > 0 && slotJobs < n {
		n = slotJobs
	}
	return n
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
		syscall.Kill(-pid, syscall.SIGKILL)
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
		os.RemoveAll(d)
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
