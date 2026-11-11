package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/loader"
)

// Up-to-date check for unchanged rebuilds. The stamp hashes everything a
// build reads: the marmot, backend and runtime core binaries, the flags,
// and the size and modification time of every Go source file (like the go
// command's file hashing shortcut). When the stamp matches the stamp of a
// previous build, its cached executable is copied and nothing else runs.

func fileSig(h interface{ Write([]byte) (int, error) }, path string) {
	if fi, err := os.Stat(path); err == nil {
		fmt.Fprintf(h, "%s %d %d\n", path, fi.Size(), fi.ModTime().UnixNano())
	} else {
		fmt.Fprintf(h, "%s missing\n", path)
	}
}

// buildStamp returns the stamp of a build, or "" if it cannot be computed.
func (b *build) buildStamp(goos, goarch string) string {
	files, goroot, err := loader.InputFiles(loader.Config{
		Dir: b.cfg.Dir, Root: b.cfg.Root, Tags: b.cfg.Tags, GOOS: goos, GOARCH: goarch, Patterns: b.cfg.Patterns,
	})
	if err != nil {
		return ""
	}
	sort.Strings(files)
	h := sha256.New()
	fmt.Fprintf(h, "stamp v1\n%s\n%s/%s\n%s %s %s %s\n%v\n", goroot, goos, goarch, b.cfg.OptLevel, b.cfg.Emit, b.cfg.Dialect, b.cfg.GC, b.cfg.Tags)
	if exe, err := os.Executable(); err == nil {
		fileSig(h, exe)
	}
	fileSig(h, filepath.Join(b.cfg.Root, "build", "bin", "marmot-backend"))
	fileSig(h, filepath.Join(b.cfg.Root, "build", "lib", "librtcore.a"))
	fileSig(h, filepath.Join(b.cfg.Root, "build", "lib", "rtcore.bc"))
	for _, k := range []string{"MARMOT_LIBSRC", "MARMOT_NO_BACKEND", "MARMOT_DIALECT", "MARMOT_KEEP_SYMBOLS", "MARMOT_LINKMAP"} {
		fmt.Fprintf(h, "%s=%s\n", k, os.Getenv(k))
	}
	for _, f := range files {
		fileSig(h, f)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func stampExe(root, stamp string) string {
	return filepath.Join(root, "build", "cache", "exe-"+stamp)
}

// upToDate copies the executable of a previous identical build to out.
func (b *build) upToDate(stamp, out string) bool {
	if stamp == "" || b.cfg.NoCache || b.cfg.Emit != "exe" || b.cfg.Work {
		return false
	}
	cached := stampExe(b.cfg.Root, stamp)
	if !fileExists(cached) {
		return false
	}
	return copyExe(cached, out) == nil
}

// saveStamp records out as the result of the build with this stamp.
func (b *build) saveStamp(stamp, out string) {
	if stamp == "" || b.cfg.NoCache || b.cfg.Emit != "exe" {
		return
	}
	cached := stampExe(b.cfg.Root, stamp)
	tmp := cached + ".tmp"
	if copyExe(out, tmp) == nil {
		os.Rename(tmp, cached)
	}
}

func copyExe(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if strings.HasSuffix(dst, ".tmp") {
		return os.WriteFile(dst, data, 0o755)
	}
	tmp := dst + ".marmot-tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
