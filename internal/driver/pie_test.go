package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// A link.sh bundle links later on a target whose C library nobody knows:
// -buildmode=pie gives a plain PIE there, never packed relative relocations
// (an ld.so older than glibc 2.36 refuses those).
func TestPieArgsBundle(t *testing.T) {
	b := &build{cfg: Config{Root: t.TempDir(), BuildMode: "pie"}, goos: "linux", goarch: "amd64"}
	if got := b.pieArgs(pieLink{bundle: true}); !slices.Equal(got, []string{"-pie"}) {
		t.Errorf("bundle: %q, want [-pie]", got)
	}
	b.cfg.BuildMode = ""
	if got := b.pieArgs(pieLink{bundle: true}); len(got) != 0 {
		t.Errorf("bundle, default mode: %q, want none", got)
	}
	b.cfg.Static = true
	b.cfg.BuildMode = "pie"
	if got := b.pieArgs(pieLink{bundle: true}); len(got) != 0 {
		t.Errorf("static: %q, want none", got)
	}
}

// A sysroot without a C library that defines GLIBC_ABI_DT_RELR (here: none
// at all) gives a plain PIE.
func TestPieArgsSysrootWithoutRELR(t *testing.T) {
	if _, err := os.Stat(llvmTool("clang")); err != nil {
		t.Skip("no clang")
	}
	sr := t.TempDir()
	b := &build{cfg: Config{Root: t.TempDir(), BuildMode: "pie"}, goos: "linux", goarch: "amd64"}
	l := pieLink{clang: []string{"--target=x86_64-unknown-linux-gnu", "--sysroot=" + sr}}
	if got := b.pieArgs(l); !slices.Equal(got, []string{"-pie"}) {
		t.Errorf("sysroot without libc.so.6: %q, want [-pie]", got)
	}
}

// On a Linux host whose C library defines GLIBC_ABI_DT_RELR, -buildmode=pie
// gives packed relative relocations, and the probe sees them: an executable
// linked with -z pack-relative-relocs has DT_RELR and needs the version, one
// linked without has neither (so the check can fail).
func TestPieArgsNativeRELR(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ELF host only")
	}
	clang := llvmTool("clang")
	if _, err := os.Stat(clang); err != nil {
		t.Skip("no clang")
	}
	if !libcHasRELR(clangFile(clang, nil, "libc.so.6")) {
		t.Skip("the host C library has no GLIBC_ABI_DT_RELR (glibc older than 2.36)")
	}
	b := &build{cfg: Config{Root: t.TempDir(), BuildMode: "pie"}, goos: "linux", goarch: runtime.GOARCH}
	want := []string{"-pie", "-Wl,-z,pack-relative-relocs"}
	if got := b.pieArgs(pieLink{clang: linuxLinker()}); !slices.Equal(got, want) {
		t.Fatalf("native: %q, want %q", got, want)
	}
	// The answer is cached: a second build reads it back.
	if m, _ := filepath.Glob(filepath.Join(b.cfg.Root, "build", "cache", "relr-*")); len(m) != 1 {
		t.Errorf("cache files: %q, want one", m)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "p.c")
	if err := os.WriteFile(src, []byte(relrProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		flag []string
		want bool
	}{
		{"relr", []string{"-Wl,-z,pack-relative-relocs"}, true},
		{"plain", nil, false},
	} {
		exe := filepath.Join(dir, c.name)
		args := append([]string{"-pie", "-fPIE", "-o", exe, src}, c.flag...)
		if out, err := exec.Command(clang, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", c.name, err, out)
		}
		if got := hasRELR(exe); got != c.want {
			t.Errorf("%s: hasRELR %v, want %v", c.name, got, c.want)
		}
		if out, err := exec.Command(exe).CombinedOutput(); err != nil {
			t.Errorf("%s: the executable does not run: %v\n%s", c.name, err, out)
		}
	}
}

// The stamp and the link use the same link: a cross build with a sysroot
// gets the PIE arguments of that sysroot's link, never the native answer.
func TestPieTargetCross(t *testing.T) {
	if _, err := os.Stat(llvmTool("clang")); err != nil {
		t.Skip("no clang")
	}
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	sr := t.TempDir()
	t.Setenv("MARMOT_SYSROOT", sr)
	b := &build{cfg: Config{Root: t.TempDir(), BuildMode: "pie"}, goos: "linux", goarch: other}
	l := b.pieTarget()
	if ldLLD() == "" {
		if !l.bundle {
			t.Fatalf("no ld.lld: %+v, want a bundle", l)
		}
	} else if l.bundle || !slices.Contains(l.clang, "--sysroot="+sr) {
		t.Fatalf("cross link: %+v, want the sysroot's link", l)
	}
	if got := b.pieArgs(l); !slices.Equal(got, []string{"-pie"}) {
		t.Errorf("cross link to a sysroot without libc.so.6: %q, want [-pie]", got)
	}
}
