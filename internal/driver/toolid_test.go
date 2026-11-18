package driver

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A rebuilt marmot-backend, marmot-pcln or rtcore.bc changes the package key
// base, so cached modules and objects built by the old tool miss. The key
// follows content: a file touched without a change keeps the key.
func TestToolIDInKeys(t *testing.T) {
	root := t.TempDir()
	tools := map[string]string{
		"backend": filepath.Join(root, "build", "bin", "marmot-backend"),
		"pcln":    filepath.Join(root, "build", "bin", "marmot-pcln"),
		"rtcore":  filepath.Join(root, "build", "lib", "rtcore.bc"),
	}
	for _, p := range tools {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("version 1"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	key := func() string {
		b := &build{cfg: Config{Root: root}, goos: runtime.GOOS, goarch: runtime.GOARCH}
		b.initKeyBase()
		return b.keyBase
	}
	base := key()
	if key() != base {
		t.Fatal("the key base differs between two builds with the same tools")
	}
	later := time.Now().Add(time.Hour)
	for name, p := range tools {
		// Same size, new content, new modification time: a rebuilt tool.
		if err := os.WriteFile(p, []byte("version 2"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, later, later); err != nil {
			t.Fatal(err)
		}
		if key() == base {
			t.Errorf("a changed %s keeps the package key base", name)
		}
		// The old content back with yet another time: the old key.
		if err := os.WriteFile(p, []byte("version 1"), 0o755); err != nil {
			t.Fatal(err)
		}
		later = later.Add(time.Hour)
		if err := os.Chtimes(p, later, later); err != nil {
			t.Fatal(err)
		}
		if got := key(); got != base {
			t.Errorf("%s with its old content and a new time: key base %s, want %s", name, got, base)
		}
	}
	// The memo files are in the cache.
	if es, _ := os.ReadDir(filepath.Join(root, "build", "cache", "filehash")); len(es) != len(tools) {
		t.Errorf("filehash memo: %d files, want %d", len(es), len(tools))
	}
}
