package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAllErrors checks that AllErrors reports every type error, in full and
// incremental loads, and that the default caps the list.
func TestAllErrors(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "src")); err != nil {
		t.Skip("needs the repository")
	}
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString("package main\n\nfunc main() {\n")
	for i := range 30 {
		fmt.Fprintf(&src, "\tvar v%d int = \"s\"\n\t_ = v%d\n", i, i)
	}
	src.WriteString("}\n")
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module errs\n\ngo 1.24\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src.String()), 0o644)
	count := func(all, incr bool) int {
		cfg := Config{Dir: dir, Root: root, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
			Patterns: []string{"."}, AllErrors: all}
		if incr {
			cfg.Incremental, cfg.CacheDir = true, t.TempDir()
		}
		_, err := Load(cfg)
		if err == nil {
			t.Fatal("no error")
		}
		return strings.Count(err.Error(), "cannot use")
	}
	for _, incr := range []bool{false, true} {
		if n := count(true, incr); n != 30 {
			t.Errorf("incremental=%v AllErrors: %d errors, want 30", incr, n)
		}
		if n := count(false, incr); n >= 30 {
			t.Errorf("incremental=%v default: %d errors, want a capped list", incr, n)
		}
	}
}
