package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/rootdir"
)

// The cache cap and trimming live in internal/rootdir.

// DefaultCacheMB is the cache cap when MARMOT_CACHE_MAX_MB is unset.
const DefaultCacheMB = rootdir.DefaultCacheMB

const trimInterval = 30 * time.Second

func cacheDir(root string) string { return rootdir.CacheDir(root) }

func touch(p string) { rootdir.Touch(p) }

// TrimCache trims the cache of the repository at root to its cap.
func TrimCache(root string, force bool) (before, after int64, err error) {
	return rootdir.TrimCache(root, force)
}

// CleanCache removes the whole cache.
func CleanCache(root string) error { return rootdir.CleanCache(root) }

// CacheSize reports the size and file count of the cache.
func CacheSize(root string) (int64, int) { return rootdir.CacheSize(root) }

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
