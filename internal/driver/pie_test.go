package driver

import (
	"debug/elf"
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
	// A linker without -z pack-relative-relocs (gold, lld before 15, GNU ld
	// before 2.38) fails the probe link: then -buildmode=pie rightly gives a
	// plain PIE, and this host cannot test the RELR case.
	dir := t.TempDir()
	src := filepath.Join(dir, "p.c")
	if err := os.WriteFile(src, []byte(relrProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	try := append(append([]string{}, linuxLinker()...), "-pie", "-fPIE", "-Wl,-z,pack-relative-relocs", "-o", filepath.Join(dir, "try"), src)
	if out, err := exec.Command(clang, try...).CombinedOutput(); err != nil {
		t.Skipf("the linker of %s has no -z pack-relative-relocs: %s", clang, out)
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

// lastPie is the last -pie or -no-pie in args: the one clang uses.
func lastPie(args []string) string {
	p := ""
	for _, a := range args {
		if a == "-pie" || a == "-no-pie" {
			p = a
		}
	}
	return p
}

// A -pie or -no-pie in #cgo LDFLAGS or MARMOT_LDFLAGS comes after marmot's
// PIE arguments on the native and the cross link, so the user's flag wins.
func TestUserPieFlagWins(t *testing.T) {
	t.Setenv("MARMOT_NOPIE", "1") // marmot's own argument: -no-pie
	t.Setenv("MARMOT_LDFLAGS", "")
	b := &build{cfg: Config{Root: t.TempDir()}, goos: "linux", goarch: runtime.GOARCH}
	cross := []string{"--target=x86_64-unknown-linux-gnu"}
	if runtime.GOOS == "linux" {
		if got := lastPie(b.link1Args([]string{"a.o"}, "rt.a", "out", nil)); got != "-no-pie" {
			t.Errorf("native, no user flag: %q, want -no-pie", got)
		}
	}
	if got := lastPie(b.crossArgs(cross, []string{"a.o"}, "rt.a", "out", nil)); got != "-no-pie" {
		t.Errorf("cross, no user flag: %q, want -no-pie", got)
	}
	b.cgoLDFLAGS = []string{"-pie"}
	if runtime.GOOS == "linux" {
		if got := lastPie(b.link1Args([]string{"a.o"}, "rt.a", "out", nil)); got != "-pie" {
			t.Errorf("native, #cgo LDFLAGS: -pie: %q, want -pie", got)
		}
	}
	if got := lastPie(b.crossArgs(cross, []string{"a.o"}, "rt.a", "out", nil)); got != "-pie" {
		t.Errorf("cross, #cgo LDFLAGS: -pie: %q, want -pie", got)
	}
	b.cgoLDFLAGS = nil
	t.Setenv("MARMOT_LDFLAGS", "-pie")
	if runtime.GOOS == "linux" {
		if got := lastPie(b.link1Args([]string{"a.o"}, "rt.a", "out", nil)); got != "-pie" {
			t.Errorf("native, MARMOT_LDFLAGS=-pie: %q, want -pie", got)
		}
	}
}

// clang takes the last of -no-pie and -pie: the argument order above decides
// the ELF type.
func TestClangLastPieWins(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ELF host only")
	}
	clang := llvmTool("clang")
	if _, err := os.Stat(clang); err != nil {
		t.Skip("no clang")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "p.c")
	if err := os.WriteFile(src, []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		flags []string
		want  elf.Type
	}{
		{"user-pie", []string{"-no-pie", "-pie"}, elf.ET_DYN},
		{"user-nopie", []string{"-pie", "-no-pie"}, elf.ET_EXEC},
	} {
		exe := filepath.Join(dir, c.name)
		args := append([]string{"-fPIE", "-o", exe, src}, c.flags...)
		args = append(args, linuxLinker()...)
		if out, err := exec.Command(clang, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", c.name, err, out)
		}
		f, err := elf.Open(exe)
		if err != nil {
			t.Fatal(err)
		}
		if f.Type != c.want {
			t.Errorf("%s: %v, want %v", c.name, f.Type, c.want)
		}
		f.Close()
	}
}
