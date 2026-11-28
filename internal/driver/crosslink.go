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

// linuxLinker picks the linker of a native Linux link. The default is the
// system's (GNU ld on the test host: 13 s of the 283 s tuios -release
// build, against 1.6 s for the pc table after it); MARMOT_LD=lld or mold
// chooses another, for measuring before it becomes the default (run time,
// size and startup must not regress, SPEC 3c).
func linuxLinker() []string {
	switch os.Getenv("MARMOT_LD") {
	case "lld":
		if p := ldLLD(); p != "" {
			return []string{"--ld-path=" + p}
		}
	case "mold":
		if p, err := exec.LookPath("mold"); err == nil {
			return []string{"--ld-path=" + p}
		}
	}
	return nil
}

// ldLLD finds ld.lld: beside marmot's clang, in Homebrew's lld, or on PATH.
func ldLLD() string {
	for _, p := range []string{llvmTool("ld.lld"), filepath.Join(filepath.Dir(homebrewLLVM), "..", "lld@22", "bin", "ld.lld"), "/opt/homebrew/opt/lld@22/bin/ld.lld", "/usr/local/opt/lld@22/bin/ld.lld"} {
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
	target := b.crossTarget()
	if target == nil {
		return false, nil
	}
	link := func(extra []string, o string) error {
		return b.run(llvmTool("clang"), b.crossArgs(target, objs, rt, o, extra)...)
	}
	if b.wantPcln() {
		return true, b.linkWithPcln(objs, link, out)
	}
	return true, link(nil, out)
}

// crossArgs is the clang arguments of a cross link to o. The user's
// #cgo LDFLAGS come after the PIE arguments, so a -pie or -no-pie there wins.
func (b *build) crossArgs(target, objs []string, rt, o string, extra []string) []string {
	args := append([]string{}, target...)
	args = append(args, "-o", o)
	args = append(args, objs...)
	args = append(args, rt, "-lpthread", "-Wl,--gc-sections", "-Wl,-z,start-stop-gc")
	if b.goos != "darwin" {
		args = append(args, b.noptrLinkArgs()...)
		args = append(args, b.pieArgs(pieLink{clang: target})...)
	}
	if b.cfg.Static {
		args = append(args, "-static")
	}
	if !b.wantPcln() {
		args = append(args, "-s")
	}
	args = append(args, b.cgoLDFLAGS...)
	return append(args, extra...)
}

// crossTarget is the clang arguments of a cross link that pick the target,
// the sysroot (so the C library), the linker and gcc's files, or nil when
// the build cannot link here (no sysroot, no ld.lld or no triple).
func (b *build) crossTarget() []string {
	sr, lld, triple := b.crossSysroot(), ldLLD(), b.cTriple()
	if sr == "" || lld == "" || triple == "" {
		return nil
	}
	args := []string{"--target=" + triple, "--sysroot=" + sr, "-fuse-ld=" + lld}
	if g := b.gccDir(sr); g != "" {
		args = append(args, "--gcc-install-dir="+g)
	}
	return args
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
