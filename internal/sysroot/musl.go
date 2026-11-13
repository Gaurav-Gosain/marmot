package sysroot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// The musl C library a static Linux sysroot is built from.
const (
	MuslVersion = "1.2.5"
	muslSHA256  = "a9a118bbe84d8764da0ea0d28b3ab3fae8477fc7e4085d90102b8596fc7c75e4"
)

var muslURL = "https://musl.libc.org/releases/musl-" + MuslVersion + ".tar.gz"

// muslTriple is the clang target musl is compiled for.
var muslTriple = map[string]string{"linux/amd64": "x86_64-linux-musl", "linux/arm64": "aarch64-linux-musl"}

// MuslArch is the goarch name of the musl sysroot of goarch, as Dir and
// Installed take it: linux-arm64-musl.
func MuslArch(goarch string) string { return goarch + "-musl" }

// AddMusl builds the musl sysroot of goos/goarch, for static executables:
// musl compiled from its release source with clang (the C compiler marmot
// uses), with the kernel headers of the Debian sysroot, which is added
// first when missing. gcc's runtime files (crtbegin.o, libgcc) stay in the
// Debian sysroot, where the link finds them.
func AddMusl(root, goos, goarch, clang string, log func(string)) (*Manifest, error) {
	target := goos + "/" + goarch
	triple, ok := muslTriple[target]
	if !ok {
		return nil, fmt.Errorf("no musl sysroot for %s (supported: %s)", target, strings.Join(Supported(), ", "))
	}
	if !Installed(root, goos, goarch) {
		if _, err := Add(root, goos, goarch, log); err != nil {
			return nil, err
		}
	}
	base := Dir(root, goos, goarch)
	dir := Dir(root, goos, MuslArch(goarch))
	tmp, err := os.MkdirTemp(filepath.Dir(dir), "tmp-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // the temporary directory of an unfinished add
	log("fetching " + muslURL)
	tgz, err := fetch(muslURL)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(tgz)
	if got := hex.EncodeToString(sum[:]); got != muslSHA256 {
		return nil, fmt.Errorf("%s: SHA256 %s, want %s", muslURL, got, muslSHA256)
	}
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "musl.tar.gz"), tgz, 0o644); err != nil {
		return nil, err
	}
	if err := runIn(tmp, nil, "tar", "-xzf", "musl.tar.gz", "-C", src, "--strip-components=1"); err != nil {
		return nil, err
	}
	out := filepath.Join(tmp, "out")
	env := []string{"CC=" + clang + " --target=" + triple, "AR=" + sibling(clang, "llvm-ar"), "RANLIB=" + sibling(clang, "llvm-ranlib")}
	log("building musl " + MuslVersion + " for " + target)
	if err := runIn(src, env, "./configure", "--target="+triple, "--prefix=/usr", "--disable-shared"); err != nil {
		return nil, err
	}
	if err := runIn(src, env, "make", "-j"+strconv.Itoa(runtime.NumCPU())); err != nil {
		return nil, err
	}
	if err := runIn(src, env, "make", "install", "DESTDIR="+out); err != nil {
		return nil, err
	}
	// The kernel headers (linux/, asm/, asm-generic/) come from Debian.
	inc := filepath.Join(out, "usr", "include")
	debInc := filepath.Join(base, "usr", "include")
	asm, _ := filepath.Glob(filepath.Join(debInc, "*-linux-gnu", "asm"))
	for _, d := range append([]string{filepath.Join(debInc, "linux"), filepath.Join(debInc, "asm-generic")}, asm...) {
		if err := runIn(tmp, nil, "cp", "-R", d, filepath.Join(inc, filepath.Base(d))); err != nil {
			return nil, err
		}
	}
	base0, _ := ReadManifest(root, goos, goarch)
	m := &Manifest{
		Target: target + " musl",
		Source: "musl " + MuslVersion + " " + muslURL + ", kernel headers of Debian " + Release,
		Added:  time.Now().UTC(),
	}
	m.Packages = append(m.Packages, Package{Name: "musl", Version: MuslVersion, SHA256: muslSHA256})
	if base0 != nil {
		for _, p := range base0.Packages {
			if p.Name == "linux-libc-dev" {
				m.Packages = append(m.Packages, p)
			}
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(out, "manifest.json"), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(dir) // an older sysroot of the target
	if err := os.Rename(out, dir); err != nil {
		return nil, err
	}
	return m, nil
}

// sibling is the LLVM tool name next to clang, or name on PATH.
func sibling(clang, name string) string {
	p := filepath.Join(filepath.Dir(clang), name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return name
}

func runIn(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		tail := string(out)
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		return fmt.Errorf("%s %s: %v\n%s", name, strings.Join(args, " "), err, tail)
	}
	return nil
}
