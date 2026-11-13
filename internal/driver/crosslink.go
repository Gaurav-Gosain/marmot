package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/sysroot"
)

// Cross builds for Linux link the executable here, with ld.lld against the
// target's sysroot (marmot sysroot add, or MARMOT_SYSROOT): crt files, the
// C library's shared objects and gcc's runtime files come from it. Without
// a sysroot or ld.lld the build writes a bundle that links on the target,
// as before.

// crossSysroot is the sysroot a cross link uses, or "".
func (b *build) crossSysroot() string {
	if b.goos != "linux" {
		return ""
	}
	if b.cfg.Static {
		if sysroot.Installed(b.cfg.Root, b.goos, sysroot.MuslArch(b.goarch)) {
			return sysroot.Dir(b.cfg.Root, b.goos, sysroot.MuslArch(b.goarch))
		}
		return ""
	}
	if sr := os.Getenv("MARMOT_SYSROOT"); sr != "" {
		return sr
	}
	if sysroot.Installed(b.cfg.Root, b.goos, b.goarch) {
		return sysroot.Dir(b.cfg.Root, b.goos, b.goarch)
	}
	return ""
}

// ldLLD finds ld.lld: beside marmot's clang, in Homebrew's lld, or on PATH.
func ldLLD() string {
	for _, p := range []string{llvmTool("ld.lld"), filepath.Join(filepath.Dir(llvmBin), "..", "lld@22", "bin", "ld.lld"), "/opt/homebrew/opt/lld@22/bin/ld.lld", "/usr/local/opt/lld@22/bin/ld.lld"} {
		if fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("ld.lld"); err == nil {
		return p
	}
	return ""
}

// linkCross links an executable for the target. ok is false when it
// cannot (no sysroot or no ld.lld).
func (b *build) linkCross(objs []string, rt, out string) (ok bool, err error) {
	sr, lld := b.crossSysroot(), ldLLD()
	triple := b.cTriple()
	if sr == "" || lld == "" || triple == "" {
		return false, nil
	}
	link := func(extra []string, o string) error {
		args := []string{"--target=" + triple, "--sysroot=" + sr, "-fuse-ld=" + lld}
		if g := b.gccDir(sr); g != "" {
			args = append(args, "--gcc-install-dir="+g)
		}
		args = append(args, "-o", o)
		args = append(args, objs...)
		args = append(args, rt, "-lpthread", "-Wl,--gc-sections", "-Wl,-z,start-stop-gc")
		if b.cfg.Static {
			args = append(args, "-static")
		}
		if !b.wantPcln() {
			args = append(args, "-s")
		}
		args = append(args, b.cgoLDFLAGS...)
		args = append(args, extra...)
		return b.run(llvmTool("clang"), args...)
	}
	if b.wantPcln() {
		return true, b.linkWithPcln(objs, link, out)
	}
	return true, link(nil, out)
}

// crossLinkNote says how to get a linked executable when a bundle was
// written instead.
func (b *build) crossLinkNote() string {
	var why []string
	if b.crossSysroot() == "" {
		if b.cfg.Static {
			why = append(why, "marmot sysroot add -libc musl "+b.goos+"/"+b.goarch)
		} else {
			why = append(why, "marmot sysroot add "+b.goos+"/"+b.goarch)
		}
	}
	if ldLLD() == "" {
		why = append(why, "install ld.lld (brew install lld@22)")
	}
	return strings.Join(why, " and ")
}

// cTriple is the clang target of the build's C and link: the musl triple
// for -static.
func (b *build) cTriple() string {
	t := cgoTriples[b.goos+"/"+b.goarch]
	if b.cfg.Static {
		t = strings.Replace(t, "-linux-gnu", "-linux-musl", 1)
	}
	return t
}

// gccDir is gcc's runtime directory (crtbegin.o, libgcc) for the link: in
// the sysroot, or for a musl sysroot in the glibc one it was made from.
func (b *build) gccDir(sr string) string {
	if g := sysroot.GCCDir(sr); g != "" {
		return g
	}
	return sysroot.GCCDir(sysroot.Dir(b.cfg.Root, b.goos, b.goarch))
}
