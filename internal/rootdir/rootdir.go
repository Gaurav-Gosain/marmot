// Package rootdir finds the marmot repository root and keeps its build
// cache under the size cap. It is small so that tools other than the
// compiler (marmot-check) can use it without linking the compiler.
package rootdir

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Find locates the marmot repository root: $MARMOT_ROOT, else three
// levels above the executable (build/bin/marmot) when that directory has
// lib/src.
func Find() (string, error) {
	if r := os.Getenv("MARMOT_ROOT"); r != "" {
		return r, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	root := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // build/bin/marmot
	if _, err := os.Stat(filepath.Join(root, "lib", "src")); err == nil {
		return root, nil
	}
	return "", fmt.Errorf("cannot find the marmot root; set MARMOT_ROOT")
}

// Cache size cap. Everything under build/cache (the MLIR
// cache, cached executables, link templates and the backend's object
// cache) shares one cap, MARMOT_CACHE_MAX_MB (default DefaultCacheMB; 0 means
// no cap). Hits touch their entry; after a build, when the cache is over the
// cap, the least recently used files are deleted until it is under 80% of
// the cap. One process trims at a time (exclusive flock on
// build/cache/.trim.lock), at most once per trimInterval unless forced.
//
// A trim must not delete an entry that a running build has looked up and
// reads later (the backend opens cached MLIR files by path). Builds look
// entries up with Use, which touches the entry under a shared lock on the
// same file, so no trim runs between the lookup and the touch. A trim keeps
// every file used in the last minEntryAge, and every file used since the
// start of the oldest running build (BeginBuild registers builds in
// build/cache/.builds), so a long build keeps what it looked up.

// DefaultCacheMB is the cache cap when MARMOT_CACHE_MAX_MB is unset.
const DefaultCacheMB = 4096

const (
	minEntryAge  = 10 * time.Minute
	trimInterval = 30 * time.Second
	buildsDir    = ".builds" // under the cache: one file per running build
)

// trimHook, for tests, runs in TrimCache between listing the entries and
// deleting them.
var trimHook func()

// Use looks up the cache entry p of the repository at root for a build
// that reads it later: it touches p and reports whether p exists, under a
// shared lock on the trim lock, so that a trim, which holds it exclusively,
// either finishes before (and p is gone) or starts after and sees p as just
// used.
func Use(root, p string) bool {
	if f, err := os.OpenFile(filepath.Join(CacheDir(root), ".trim.lock"), os.O_RDWR|os.O_CREATE, 0o666); err == nil {
		defer func() { _ = f.Close() }() // the lock file holds nothing
		if lockShared(f) {
			defer unlockFile(f)
		}
	}
	Touch(p)
	_, err := os.Stat(p)
	return err == nil
}

// BeginBuild registers a running build of the repository at root, so that
// trims keep the cache files it uses for as long as it runs; the returned
// function ends the registration.
func BeginBuild(root string) func() {
	dir := filepath.Join(CacheDir(root), buildsDir)
	if os.MkdirAll(dir, 0o755) != nil {
		return func() {}
	}
	p := filepath.Join(dir, fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano()))
	if os.WriteFile(p, nil, 0o644) != nil {
		return func() {}
	}
	return func() { _ = os.Remove(p) } // a file left behind is dropped once its process is gone
}

// oldestBuild is the start of the oldest running build registered in the
// cache dir. It removes the files of builds whose process is gone.
func oldestBuild(dir string) (time.Time, bool) {
	dir = filepath.Join(dir, buildsDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, false
	}
	var oldest time.Time
	found := false
	for _, e := range ents {
		pid, _, _ := strings.Cut(e.Name(), "-")
		n, err := strconv.Atoi(pid)
		if err != nil {
			continue
		}
		if !processAlive(n) {
			_ = os.Remove(filepath.Join(dir, e.Name())) // stale: its build died
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !found || info.ModTime().Before(oldest) {
			oldest, found = info.ModTime(), true
		}
	}
	return oldest, found
}

// CacheCap is the cap in bytes, 0 for none.
func CacheCap() int64 {
	if v, err := strconv.ParseInt(os.Getenv("MARMOT_CACHE_MAX_MB"), 10, 64); err == nil && v >= 0 {
		return v << 20
	}
	return DefaultCacheMB << 20
}

// CacheDir is the cache directory of the repository at root.
func CacheDir(root string) string { return filepath.Join(root, "build", "cache") }

// Touch marks a cache entry as used now.
func Touch(p string) {
	now := time.Now()
	_ = os.Chtimes(p, now, now) // a missed mark only lets the entry age out sooner
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
	// The callback skips what it cannot read, so the walk cannot fail.
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p == filepath.Join(dir, buildsDir) {
				return filepath.SkipDir // registrations of running builds, not entries
			}
			if p == filepath.Join(dir, "sysroot") {
				// Installed by marmot sysroot add, not a cache: its files
				// keep the packages' old times and would go first.
				return filepath.SkipDir
			}
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
	dir := CacheDir(root)
	limit := CacheCap()
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
	defer func() { _ = lf.Close() }() // the lock file holds nothing
	if !lockFile(lf, !force) {
		return 0, 0, nil
	}
	defer unlockFile(lf) // closing the file releases it anyway
	Touch(lockPath)
	entries, total := cacheEntries(dir)
	before, after = total, total
	if limit == 0 || total <= limit {
		return before, after, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.Before(entries[j].mod) })
	goal := limit / 10 * 8
	keep := time.Now().Add(-minEntryAge) // files used since then stay
	if t, ok := oldestBuild(dir); ok && t.Before(keep) {
		keep = t
	}
	if trimHook != nil {
		trimHook()
	}
	for _, e := range entries {
		if after <= goal {
			break
		}
		if !e.mod.Before(keep) {
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
	return os.RemoveAll(CacheDir(root))
}

// CacheSize reports the size and file count of the cache.
func CacheSize(root string) (int64, int) {
	e, total := cacheEntries(CacheDir(root))
	return total, len(e)
}
