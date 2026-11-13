package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Background tier 1: a dev build (tier0) leaves the modules its backend
// compiled in a spool; after the build returns, a marmot-backend started
// in its own session compiles them at -O2 on idle cores and caches the
// objects, which the next build links in place of the tier0 ones. A build
// never waits for it, and a second runner leaves the spool to the first.
// The runner's cache keys come from the same backend options as the
// build's, so an object only serves builds made with those options.

// tier1Default is whether dev builds use background tier 1 when
// MARMOT_TIER1 is not set. MARMOT_TIER1=0 turns it off; the conformance,
// fuzz and bench harnesses set that.
const tier1Default = true

// tier1 reports whether this build spools its modules for background
// tier 1 and links the objects tier 1 made: tier0 builds with a cache.
// MARMOT_TIER1=1 turns it on, MARMOT_TIER1=0 off. MARMOT_TIER1=sync is
// on, and the build waits for the runner (tests: every later build of a
// test run links the -O2 objects of the packages earlier builds compiled).
func (b *build) tier1() bool {
	if !b.tier0() || b.cfg.NoCache || !b.haveBackend() {
		return false
	}
	switch os.Getenv("MARMOT_TIER1") {
	case "1", "sync":
		return true
	case "0":
		return false
	}
	return tier1Default
}

func (b *build) tier1Spool() string {
	return filepath.Join(cacheDir(b.cfg.Root), "tier1")
}

// startTier1 starts the background runner when the spool has modules and
// returns without waiting. Its messages go to tier1.log in the spool.
func (b *build) startTier1() {
	if !b.tier1() {
		return
	}
	// Nothing spooled (every module came from the cache): no runner.
	if m, _ := filepath.Glob(filepath.Join(b.tier1Spool(), "*.mlir")); len(m) == 0 {
		return
	}
	var args []string
	for _, a := range b.backendArgs("obj", "-") {
		if a != "--batch=-" && a != "-time" {
			args = append(args, a)
		}
	}
	args = append(args, "-tier1-run")
	backend := filepath.Join(b.cfg.Root, "build", "bin", "marmot-backend")
	cmd := exec.Command(backend, args...)
	// Its own session: not killed with the build's process group, and no
	// terminal signals.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f, err := os.Create(filepath.Join(b.tier1Spool(), "tier1.log")); err == nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer func() { _ = f.Close() }() // the child has its own copy of the log file
	}
	if err := cmd.Start(); err != nil {
		b.logf("tier1: %v", err)
		return
	}
	if os.Getenv("MARMOT_TIER1") == "sync" {
		if err := cmd.Wait(); err != nil {
			b.logf("tier1: %v", err)
		}
		b.logf("tier1: done")
		return
	}
	b.logf("tier1: started")
	// Reaped when it exits (a long-lived process would keep a zombie).
	go func() { _ = cmd.Wait() }() // tier 1 reports through its log
}
