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

// oldEntry writes a cache file of n bytes last used two days ago.
func oldEntry(t *testing.T, root, rel string, n int) string {
	t.Helper()
	p := filepath.Join(CacheDir(root), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	return p
}

// A build looks up an old entry while another process trims: the trim has
// listed the entry as old and deletes it after the lookup said it exists,
// and the backend then cannot open it (fuzz 065834 seed 4251). With Use, the
// lookup waits for the trim, so a lookup that reports the entry finds it.
func TestUseDuringTrim(t *testing.T) {
	root := t.TempDir()
	p := oldEntry(t, root, "mlir/aa.mlir", 2<<20)
	oldEntry(t, root, "mlir/bb.mlir", 2<<20)
	t.Setenv("MARMOT_CACHE_MAX_MB", "1")
	found := make(chan bool, 1)
	trimHook = func() {
		go func() { found <- Use(root, p) }()
		select { // the old lookup ran here at once; Use waits for the lock
		case ok := <-found:
			found <- ok
		case <-time.After(300 * time.Millisecond):
		}
	}
	defer func() { trimHook = nil }()
	if _, _, err := TrimCache(root, true); err != nil {
		t.Fatal(err)
	}
	ok := <-found
	if _, err := os.Stat(p); ok && err != nil {
		t.Errorf("Use reported %s, and the trim deleted it after", p)
	}
}

// A trim keeps the files a running build has used since it started, even
// past minEntryAge, and drops them once the build ends.
func TestTrimKeepsFilesOfRunningBuilds(t *testing.T) {
	root := t.TempDir()
	end := BeginBuild(root)
	ents, err := os.ReadDir(filepath.Join(CacheDir(root), buildsDir))
	if err != nil || len(ents) != 1 {
		t.Fatalf("registration: %v %v", ents, err)
	}
	reg := filepath.Join(CacheDir(root), buildsDir, ents[0].Name())
	start := time.Now().Add(-3 * time.Hour) // a long build
	if err := os.Chtimes(reg, start, start); err != nil {
		t.Fatal(err)
	}
	used := oldEntry(t, root, "mlir/used.mlir", 2<<20)
	hour := time.Now().Add(-time.Hour) // used by the build, older than minEntryAge
	if err := os.Chtimes(used, hour, hour); err != nil {
		t.Fatal(err)
	}
	stale := oldEntry(t, root, "mlir/stale.mlir", 2<<20)
	t.Setenv("MARMOT_CACHE_MAX_MB", "1")
	if _, _, err := TrimCache(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(used); err != nil {
		t.Error("a trim deleted a file a running build used")
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a trim kept a file from before the oldest running build")
	}
	end()
	if _, _, err := TrimCache(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(used); err == nil {
		t.Error("a trim kept the file after its build ended")
	}
}
