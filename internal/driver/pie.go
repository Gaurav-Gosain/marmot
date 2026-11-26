package driver

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// pieArgs are the ELF linker arguments for the kind of executable.
//
// A position independent executable has a relative relocation for every
// address in its data (tuios: 556k entries, a 13 MB .rela.dyn that ld.so
// reads at startup, and all of .data written while relocating). A position
// dependent one (-no-pie, as go build makes on linux) resolves them at link
// time; MARMOT_NOPIE=1 asks for it, for now (the default once measured).
// The objects stay position independent; the linker relaxes their GOT
// references. -buildmode=pie keeps a PIE, with packed relative relocations
// (RELR: the table shrinks to a few KB; .data is still written at startup)
// where the link can make them and the target's ld.so reads them (relrOK).
//
// l is the link the arguments are for: the native link, a cross link with
// a sysroot, or a link.sh bundle (linked later on a target nobody knows).
func (b *build) pieArgs(l pieLink) []string {
	if b.cfg.Static {
		return nil // static executables are position dependent already
	}
	if b.cfg.Race {
		return []string{"-no-pie"} // TSan's Go mapping wants globals low (race.go)
	}
	switch b.cfg.BuildMode {
	case "pie":
		if !l.bundle && b.relrOK(l.clang) {
			return []string{"-pie", "-Wl,-z,pack-relative-relocs"}
		}
		return []string{"-pie"}
	}
	if os.Getenv("MARMOT_NOPIE") == "1" {
		return []string{"-no-pie"}
	}
	return nil
}

// pieLink says which link pieArgs is for. clang holds the arguments that
// pick the target, the sysroot and the linker (--target, --sysroot,
// -fuse-ld, --ld-path); bundle is a link.sh bundle.
type pieLink struct {
	clang  []string
	bundle bool
}

// relrDT is GLIBC_ABI_DT_RELR: glibc 2.36 and later define this version, and
// lld and GNU ld make an executable with packed relative relocations need it,
// so an older ld.so refuses the executable instead of running it with its
// pointers unrelocated.
const relrDT = "GLIBC_ABI_DT_RELR"

// dtRELR is the DT_RELR dynamic tag (debug/elf has no name for it).
const dtRELR = elf.DynTag(36)

var relrMemo sync.Map // key -> bool

// relrOK reports whether a link with these clang arguments can make packed
// relative relocations that the target loads: the C library the link uses
// (libc.so.6) defines GLIBC_ABI_DT_RELR, and a probe link with
// -z pack-relative-relocs gives an executable with DT_RELR that needs that
// version (an old linker only warns about the flag and links without it).
// The answer is cached in build/cache, keyed by clang, the arguments, the
// C library and the linker the arguments name.
func (b *build) relrOK(args []string) bool {
	clang := llvmTool("clang")
	libc := clangFile(clang, args, "libc.so.6")
	h := sha256.New()
	fileSig(h, clang)
	fmt.Fprintf(h, "%q\n", args)
	fileSig(h, libc)
	if p := linkerPath(clang, args); p != "" {
		fileSig(h, p)
	}
	key := hex.EncodeToString(h.Sum(nil))[:24]
	if v, ok := relrMemo.Load(key); ok {
		return v.(bool)
	}
	dir := filepath.Join(b.cfg.Root, "build", "cache")
	cache := filepath.Join(dir, "relr-"+key)
	if data, err := os.ReadFile(cache); err == nil {
		ok := strings.TrimSpace(string(data)) == "1"
		relrMemo.Store(key, ok)
		return ok
	}
	ok := libcHasRELR(libc) && probeRELR(clang, args)
	if err := os.MkdirAll(dir, 0o755); err == nil {
		v := "0"
		if ok {
			v = "1"
		}
		_ = os.WriteFile(cache, []byte(v+"\n"), 0o644)
	}
	relrMemo.Store(key, ok)
	if !ok {
		b.logf("-buildmode=pie: a PIE without packed relative relocations (the C library or the linker of this link has no DT_RELR)")
	}
	return ok
}

// clangFile is the file of that name the link would use (clang
// -print-file-name), or "" when clang does not find it.
func clangFile(clang string, args []string, name string) string {
	out, err := exec.Command(clang, append(append([]string{}, args...), "-print-file-name="+name)...).Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == name || !filepath.IsAbs(p) {
		return "" // clang prints the name itself when it finds no such file
	}
	return p
}

// linkerPath is the linker the arguments name (--ld-path=, -fuse-ld= with a
// path), or clang's default ld, for the cache key.
func linkerPath(clang string, args []string) string {
	for _, a := range args {
		for _, p := range []string{"--ld-path=", "-fuse-ld="} {
			if v, ok := strings.CutPrefix(a, p); ok && filepath.IsAbs(v) {
				return v
			}
		}
	}
	out, err := exec.Command(clang, append(append([]string{}, args...), "-print-prog-name=ld")...).Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if !filepath.IsAbs(p) {
		if lp, err := exec.LookPath(p); err == nil {
			return lp
		}
		return ""
	}
	return p
}

// libcHasRELR reports whether the shared C library at path defines
// GLIBC_ABI_DT_RELR, the check ld.so makes.
func libcHasRELR(path string) bool {
	if path == "" {
		return false
	}
	f, err := elf.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	vers, err := f.DynamicVersions()
	if err != nil {
		return false
	}
	for _, v := range vers {
		if v.Name == relrDT {
			return true
		}
	}
	return false
}

// relrProbe has addresses in its data, so the probe executable has relative
// relocations to pack.
const relrProbe = "static int x[4];\nint *p[4] = {&x[0], &x[1], &x[2], &x[3]};\nint main(void) { return *p[0]; }\n"

// probeRELR links a small program with -z pack-relative-relocs and checks
// that the executable has DT_RELR and needs GLIBC_ABI_DT_RELR.
func probeRELR(clang string, args []string) bool {
	dir, err := os.MkdirTemp("", "marmot-relr-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	src, exe := filepath.Join(dir, "p.c"), filepath.Join(dir, "p")
	if err := os.WriteFile(src, []byte(relrProbe), 0o644); err != nil {
		return false
	}
	cmd := append(append([]string{}, args...), "-pie", "-fPIE", "-Wl,-z,pack-relative-relocs", "-o", exe, src)
	if err := exec.Command(clang, cmd...).Run(); err != nil {
		return false
	}
	return hasRELR(exe)
}

// hasRELR reports whether the executable at path has packed relative
// relocations (DT_RELR) and needs GLIBC_ABI_DT_RELR.
func hasRELR(path string) bool {
	f, err := elf.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if v, err := f.DynValue(dtRELR); err != nil || len(v) == 0 {
		return false
	}
	needs, err := f.DynamicVersionNeeds()
	if err != nil {
		return false
	}
	for _, n := range needs {
		for _, d := range n.Needs {
			if d.Dep == relrDT {
				return true
			}
		}
	}
	return false
}

// checkBuildMode rejects an unknown -buildmode.
func checkBuildMode(m string) error {
	switch m {
	case "", "exe", "pie":
		return nil
	}
	return fmt.Errorf("-buildmode %s: only exe and pie", m)
}
