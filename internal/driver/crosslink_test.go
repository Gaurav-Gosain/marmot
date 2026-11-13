package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -static builds use the musl triple, the musl sysroot and runtime, and
// gcc's files from the glibc sysroot.
func TestStaticTargets(t *testing.T) {
	root := t.TempDir()
	mk := func(p string) {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("build/cache/sysroot/linux-arm64/usr/lib/gcc/aarch64-linux-gnu/12")
	mk("build/cache/sysroot/linux-arm64-musl")
	if err := os.WriteFile(filepath.Join(root, "build/cache/sysroot/linux-arm64-musl/manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &build{cfg: Config{Root: root, Static: true}, goos: "linux", goarch: "arm64"}
	if got := b.cTriple(); got != "aarch64-linux-musl" {
		t.Errorf("cTriple = %s", got)
	}
	if got, want := b.rtLib("librtcore.a"), filepath.Join(root, "build/lib/linux-arm64-musl/librtcore.a"); got != want {
		t.Errorf("rtLib = %s, want %s", got, want)
	}
	sr := b.crossSysroot()
	if want := filepath.Join(root, "build/cache/sysroot/linux-arm64-musl"); sr != want {
		t.Errorf("crossSysroot = %s, want %s", sr, want)
	}
	if got, want := b.gccDir(sr), filepath.Join(root, "build/cache/sysroot/linux-arm64/usr/lib/gcc/aarch64-linux-gnu/12"); got != want {
		t.Errorf("gccDir = %s, want %s", got, want)
	}
	b.cfg.Static = false
	if got := b.cTriple(); got != "aarch64-linux-gnu" {
		t.Errorf("cTriple without -static = %s", got)
	}
	if sr := b.crossSysroot(); sr != "" {
		t.Errorf("crossSysroot without the glibc sysroot's manifest = %s", sr)
	}
}

func TestStaticNeedsLinux(t *testing.T) {
	_, err := Build(Config{Root: t.TempDir(), Static: true, Target: "darwin/arm64", Patterns: []string{"."}})
	if err == nil || !strings.Contains(err.Error(), "-static links Linux executables") {
		t.Fatalf("-static for darwin: %v", err)
	}
}
