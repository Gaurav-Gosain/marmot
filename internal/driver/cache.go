package driver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Cache size cap. Everything under build/cache (the MLIR
// cache, cached executables, link templates and the backend's object
// cache) shares one cap, MARMOT_CACHE_MAX_MB (default DefaultCacheMB; 0 means
// no cap). Hits touch their entry; after a build, when the cache is over the
// cap, the least recently used files are deleted until it is under 80% of
// the cap. Files used in the last minEntryAge are never deleted, so an entry
// another marmot process has just looked up stays until it is read. One
// process trims at a time (flock on build/cache/.trim.lock), at most once
// per trimInterval unless forced.

// DefaultCacheMB is the cache cap when MARMOT_CACHE_MAX_MB is unset.
const DefaultCacheMB = 4096

const (
	minEntryAge  = 10 * time.Minute
	trimInterval = 30 * time.Second
)

func cacheCapBytes() int64 {
	if v, err := strconv.ParseInt(os.Getenv("MARMOT_CACHE_MAX_MB"), 10, 64); err == nil && v >= 0 {
		return v << 20
	}
	return DefaultCacheMB << 20
}

func cacheDir(root string) string { return filepath.Join(root, "build", "cache") }

// touch marks a cache entry as used now.
func touch(p string) {
	now := time.Now()
	os.Chtimes(p, now, now)
}

type cacheEntry struct {
	path string
	size int64
	mod  time.Time
}

// cacheEntries lists the files under the cache directory.
func cacheEntries(dir string) ([]cacheEntry, int64) {
	var out []cacheEntry
	var total int64
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".trim") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, cacheEntry{p, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	return out, total
}

// TrimCache trims the cache of the repository at root to its cap. Without
// force it returns at once if another process is trimming or a trim ran in
// the last trimInterval. It returns the cache size before and after.
func TrimCache(root string, force bool) (before, after int64, err error) {
	dir := cacheDir(root)
	limit := cacheCapBytes()
	if limit == 0 && !force {
		return 0, 0, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, 0, err
	}
	lockPath := filepath.Join(dir, ".trim.lock")
	if !force {
		if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) < trimInterval {
			return 0, 0, nil
		}
	}
	lf, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return 0, 0, err
	}
	defer lf.Close()
	how := syscall.LOCK_EX
	if !force {
		how |= syscall.LOCK_NB
	}
	if syscall.Flock(int(lf.Fd()), how) != nil {
		return 0, 0, nil
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	touch(lockPath)
	entries, total := cacheEntries(dir)
	before, after = total, total
	if limit == 0 || total <= limit {
		return before, after, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.Before(entries[j].mod) })
	goal := limit / 10 * 8
	now := time.Now()
	for _, e := range entries {
		if after <= goal {
			break
		}
		if now.Sub(e.mod) < minEntryAge {
			break // sorted: everything after is newer
		}
		if os.Remove(e.path) == nil {
			after -= e.size
		}
	}
	return before, after, nil
}

// CleanCache removes the whole cache.
func CleanCache(root string) error {
	return os.RemoveAll(cacheDir(root))
}

// CacheSize reports the size and file count of the cache.
func CacheSize(root string) (int64, int) {
	e, total := cacheEntries(cacheDir(root))
	return total, len(e)
}

// Work directories. Each build's temp dir holds a pid file; a dir is
// removed when the build ends (also on SIGINT/TERM/HUP), and builds sweep
// dirs left by processes that died (SIGKILL, crash). -work dirs hold a
// keep file and stay.

const workPrefix = "marmot-"

func markWorkDir(dir string, keep bool) {
	os.WriteFile(filepath.Join(dir, ".pid"), []byte(strconv.Itoa(os.Getpid())), 0o644)
	if keep {
		os.WriteFile(filepath.Join(dir, ".keep"), nil, 0o644)
	}
}

// sweepWorkDirs removes work dirs of dead marmot processes.
func sweepWorkDirs(logf func(string, ...any)) {
	tmp := os.TempDir()
	stamp := filepath.Join(tmp, ".marmot-sweep")
	if info, err := os.Stat(stamp); err == nil && time.Since(info.ModTime()) < trimInterval {
		return
	}
	os.WriteFile(stamp, nil, 0o644)
	ents, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), workPrefix) {
			continue
		}
		d := filepath.Join(tmp, e.Name())
		if fileExists(filepath.Join(d, ".keep")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		age := time.Since(info.ModTime())
		if age < time.Hour {
			continue // written to recently: a build may be using it whatever its pid says
		}
		if data, err := os.ReadFile(filepath.Join(d, ".pid")); err == nil {
			pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			if err := syscall.Kill(pid, 0); pid > 0 && (err == nil || err == syscall.EPERM) && age < 24*time.Hour {
				continue // alive (a reused pid ages out after a day)
			}
		} else if age < 6*time.Hour {
			continue // no pid file: an older marmot or a dir being created
		}
		if os.RemoveAll(d) == nil {
			n++
		}
	}
	if n > 0 && logf != nil {
		logf("removed %d stale work dirs", n)
	}
}

func fmtMB(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }
