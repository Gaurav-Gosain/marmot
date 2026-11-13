package rootdir

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Trimming removes old cache files but never the sysroots, whose files
// keep the old times of the packages they come from.
func TestTrimKeepsSysroots(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	write := func(rel string, n int) string {
		p := filepath.Join(CacheDir(root), rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	}
	obj := write("backend/a.o", 2<<20)
	hdr := write("sysroot/linux-arm64/usr/include/stdio.h", 2<<20)
	t.Setenv("MARMOT_CACHE_MAX_MB", "1")
	if _, _, err := TrimCache(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(obj); err == nil {
		t.Error("an old cache file over the cap stayed")
	}
	if _, err := os.Stat(hdr); err != nil {
		t.Error("trimming removed a sysroot file")
	}
}
