package driver

import (
	"crypto/sha256"
	"debug/macho"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/loader"
)

// The pc table: after the link, build the table from the
// DWARF line tables the backend emitted for the frontend's locations and
// put it in the binary (Mach-O: the end of __TEXT, so a small table
// fits in the segment's free tail). -s (Config.Strip) or MARMOT_STRIP=1 skips it.

func (b *build) wantPcln() bool {
	if b.cfg.Strip || os.Getenv("MARMOT_STRIP") != "" || os.Getenv("MARMOT_NO_LOCS") != "" || b.cfg.Emit != "exe" {
		return false
	}
	// A cross build gets the table when it links here (linkCross), not
	// when it writes a bundle to link on the target.
	if b.cross() && (b.crossSysroot() == "" || ldLLD() == "") {
		return false
	}
	return fileExists(b.pclnTool())
}

// pclnV3 turns on version 3 tables (inlined frames) for every build:
// runtime.Caller counts logical frames, as with gc (log.Lshortfile asks
// for the caller of an inlined Logger.output). It cost 10% of a release
// cmd/marmot binary.
const pclnV3 = true

// pclnArgs are the table builder's arguments before the inputs.
// MARMOT_PCLN_VERSION overrides the version.
func (b *build) pclnArgs(tbl string) []string {
	return append([]string{"-o", tbl}, b.pclnVersionArgs()...)
}

func (b *build) pclnVersionArgs() []string {
	v := os.Getenv("MARMOT_PCLN_VERSION")
	if v == "" && pclnV3 {
		v = "3"
	}
	if v != "" {
		return []string{"-table-version=" + v}
	}
	return nil
}

// fullTrace: every call site keeps its own inlined frames (LLVM does not
// merge calls with different locations): dev builds and -release
// -trace=full.
func (b *build) fullTrace() bool { return pclnV3 && (!b.cfg.Release || b.cfg.Trace == "full") }

// pclnFrags: the backend writes a pc table fragment next to each object and
// the table goes into the binary with one link and a patch (
// "Fragments"; macOS). MARMOT_PCLN_DSYM=1 uses the dsymutil path instead.
// On ELF only with tier0: its objects have no DWARF to build the table from.
func (b *build) pclnFrags() bool {
	if !b.wantPcln() || os.Getenv("MARMOT_PCLN_DSYM") != "" {
		return false
	}
	return b.goos == "darwin" || b.tier0() // the output's format: the target's
}

// tier0 reports whether the backend compiles with tier0 (-tier0): dev builds
// with no -O level, never -release; MARMOT_TIER0=0 turns it off.
func (b *build) tier0() bool {
	return !b.wpo() && !b.cfg.Release && !b.cfg.OptSet && os.Getenv("MARMOT_TIER0") != "0"
}

// backendBuildArgs are the backend options that follow from the build mode.
func (b *build) backendBuildArgs(emit string) []string {
	var a []string
	if emit == "exe" || emit == "obj" {
		// Large modules as function groups in parallel, cached one by one
		// ("Large modules"); with -wpo the groups are cut after the
		// whole-program rewrite (an edit of cmd/marmot's main package
		// recompiled its 4.3 MB module on one thread). MARMOT_SPLIT=N
		// overrides the size, 0 turns it off.
		split := "20000"
		if v := os.Getenv("MARMOT_SPLIT"); v != "" {
			split = v
		}
		if split != "0" && b.cfg.Emit == "exe" {
			a = append(a, "-split-large="+split)
		}
	}
	// Dev builds go through the tier0 code generator (LLVM -O0 for the
	// functions it leaves).
	if b.tier0() && (emit == "exe" || emit == "obj") {
		a = append(a, "-tier0")
	}
	if b.pclnFrags() {
		a = append(a, "-pcln-frags")
	}
	if b.fullTrace() {
		a = append(a, "-keep-call-sites")
	}
	return a
}

func (b *build) pclnTool() string {
	return filepath.Join(b.cfg.Root, "build", "bin", "marmot-pcln")
}

// linkWithPcln links out with the table. link(args) runs the linker with
// the given extra arguments and output.
func (b *build) linkWithPcln(objs []string, link func(extra []string, out string) error, out string) error {
	if b.pclnFrags() && b.goos != "darwin" {
		return b.linkWithFragsELF(objs, link, out)
	}
	if b.pclnFrags() {
		err := b.linkWithFrags(objs, link, out)
		if err == nil {
			return nil
		}
		b.logf("pcln: fragments: %v (dsymutil path)", err)
	}
	tmp := filepath.Join(b.work, "nopcln.exe")
	if b.goos != "darwin" {
		// ELF: the DWARF is in the binary.
		if err := link(nil, tmp); err != nil {
			return err
		}
		tbl := filepath.Join(b.work, "pcln.bin")
		if err := b.run(b.pclnTool(), append(b.pclnArgs(tbl), tmp)...); err != nil {
			b.logf("pcln: %v (no table)", err)
			return b.run(llvmTool("llvm-objcopy"), "--strip-all", tmp, out)
		}
		origins := b.cOriginSectionArgs(tmp, b.rtLib("librtcore.a"))
		if err := b.run(llvmTool("llvm-objcopy"), "--strip-all", tmp, tmp+".s"); err != nil {
			return err
		}
		return b.run(llvmTool("llvm-objcopy"), append(append([]string{"--add-section", ".marmot_pcln=" + tbl}, origins...), tmp+".s", out)...)
	}
	// The first link carries a placeholder section so that both links have
	// the same load commands and __TEXT does not move.
	ph := filepath.Join(b.work, "pcln.placeholder")
	if err := os.WriteFile(ph, []byte{0}, 0o644); err != nil {
		return err
	}
	// marmot-pcln needs the local (C) symbols to end Go function ranges.
	b.keepLocals = true
	err := link([]string{"-Wl,-sectcreate,__TEXT,__marmot_pcln," + ph}, tmp)
	b.keepLocals = false
	if err != nil {
		return err
	}
	dsym := filepath.Join(b.work, "nopcln.dSYM")
	tbl := filepath.Join(b.work, "pcln.bin")
	if err := b.run("xcrun", "dsymutil", tmp, "-o", dsym); err != nil {
		b.logf("pcln: dsymutil: %v (no table)", err)
		return os.Rename(tmp, out)
	}
	dwarf := filepath.Join(dsym, "Contents", "Resources", "DWARF", filepath.Base(tmp))
	if err := b.run(b.pclnTool(), append(b.pclnArgs(tbl), "-base-from", tmp, dwarf)...); err != nil {
		b.logf("pcln: %v (no table)", err)
		return os.Rename(tmp, out)
	}
	if t := b.cOriginTable(tmp, b.rtLib("librtcore.a")); t != nil {
		if f, err := os.OpenFile(tbl, os.O_APPEND|os.O_WRONLY, 0); err == nil {
			_, _ = f.Write(t) // best effort: marmot size falls back to "C"
			_ = f.Close()
		}
	}
	if err := link([]string{"-Wl,-sectcreate,__TEXT,__marmot_pcln," + tbl, "-Wl,-S"}, out); err != nil {
		return err
	}
	if same, err := sameText(tmp, out); err != nil || !same {
		b.logf("pcln: code moved in the relink (%v); no table", err)
		return os.Rename(tmp, out)
	}
	b.logf("pcln table added")
	return nil
}

// linkWithFragsELF links once, builds the table from the objects' fragments
// and the binary's symbols, and adds it as a section after stripping.
func (b *build) linkWithFragsELF(objs []string, link func(extra []string, out string) error, out string) error {
	tmp := filepath.Join(b.work, "nopcln.exe")
	if err := link(nil, tmp); err != nil {
		return err
	}
	list, err := b.writeFragList(objs)
	if err == nil {
		tbl := filepath.Join(b.work, "pcln.bin")
		args := append([]string{"-frags", list}, b.pclnVersionArgs()...)
		if err = b.run(b.pclnTool(), append(args, "-binary", tmp, "-o", tbl)...); err == nil {
			origins := b.cOriginSectionArgs(tmp, b.rtLib("librtcore.a"))
			if err := b.run(llvmTool("llvm-objcopy"), "--strip-all", tmp, tmp+".s"); err != nil {
				return err
			}
			b.logf("pcln table added (fragments)")
			return b.run(llvmTool("llvm-objcopy"), append(append([]string{"--add-section", ".marmot_pcln=" + tbl}, origins...), tmp+".s", out)...)
		}
	}
	b.logf("pcln: fragments: %v (no table)", err)
	return b.run(llvmTool("llvm-objcopy"), "--strip-all", tmp, out)
}

// writeFragList writes the list of the objects' fragment files.
func (b *build) writeFragList(objs []string) (string, error) {
	var frags strings.Builder
	for _, o := range objs {
		if fileExists(o + ".pcln") {
			frags.WriteString(o + ".pcln\n")
		}
	}
	if frags.Len() == 0 {
		return "", fmt.Errorf("no fragments")
	}
	list := filepath.Join(b.work, "pcln.frags")
	return list, os.WriteFile(list, []byte(frags.String()), 0o644)
}

// sameText reports whether two Mach-O files have their code at the same
// addresses (the table describes the first file's __text).
func sameText(a, b string) (bool, error) {
	fa, err := macho.Open(a)
	if err != nil {
		return false, err
	}
	defer func() { _ = fa.Close() }() // read only
	fb, err := macho.Open(b)
	if err != nil {
		return false, err
	}
	defer func() { _ = fb.Close() }() // read only
	sa, sb := fa.Section("__text"), fb.Section("__text")
	if sa == nil || sb == nil {
		return false, fmt.Errorf("no __text")
	}
	return sa.Addr == sb.Addr && sa.Size == sb.Size, nil
}

var needsRe = regexp.MustCompile(`the table needs (\d+) bytes`)

// droppedRe is marmot-pcln's report that -strip-locals dropped the locals.
var droppedRe = regexp.MustCompile(`[1-9][0-9]* local symbols dropped`)

// linkWithFrags links once with a zero filled placeholder section and writes
// the table from the objects' fragments into it (marmot-pcln -patch). The
// placeholder is the previous build's table size for this output plus 10%
// and 4 KB (an edit changes the table little); if the table does not fit,
// one more link with the size marmot-pcln asks for.
func (b *build) linkWithFrags(objs []string, link func(extra []string, out string) error, out string) error {
	list, err := b.writeFragList(objs)
	if err != nil {
		return err
	}
	abs, _ := filepath.Abs(out)
	sum := sha256.Sum256([]byte(abs))
	sizeFile := filepath.Join(b.cfg.Root, "build", "cache", "pcln-size-"+hex.EncodeToString(sum[:8]))
	size := 0
	if data, err := os.ReadFile(sizeFile); err == nil {
		size, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	reserve := 1
	if size > 0 {
		reserve = size + size/10 + 4096
	}
	tmp := filepath.Join(b.work, "pcln.exe")
	ph := filepath.Join(b.work, "pcln.placeholder")
	for attempt := range 2 {
		if err := os.WriteFile(ph, make([]byte, reserve), 0o644); err != nil {
			return err
		}
		// marmot-pcln reads function addresses from the symbol table; no
		// debug map is needed (-S).
		b.keepLocals = true
		err := link([]string{"-Wl,-sectcreate,__TEXT,__marmot_pcln," + ph, "-Wl,-S"}, tmp)
		b.keepLocals = false
		if err != nil {
			return err
		}
		args := append([]string{"-frags", list}, b.pclnVersionArgs()...)
		args = append(args, "-patch", tmp)
		keep := os.Getenv("MARMOT_KEEP_SYMBOLS") != ""
		if !keep {
			args = append(args, "-strip-locals")
		}
		err = b.run(b.pclnTool(), args...)
		if err == nil {
			if m := regexp.MustCompile(`(\d+) bytes`).FindStringSubmatch(b.lastOutput); m != nil {
				// Whole or not at all: a cut size would read as a valid one.
				if err := os.MkdirAll(filepath.Dir(sizeFile), 0o755); err != nil {
					b.logf("pcln size cache: %v", err)
				} else if err := loader.WriteAtomic(sizeFile, []byte(m[1]+"\n")); err != nil {
					b.logf("pcln size cache: %v", err)
				}
			}
			b.logf("pcln table added (fragments, %d link(s))", attempt+1)
			if m := regexp.MustCompile(`(\d+) bytes`).FindStringSubmatch(b.lastOutput); m != nil {
				used, _ := strconv.Atoi(m[1])
				b.patchCOriginMacho(tmp, b.rtLib("librtcore.a"), used)
			}
			// The link kept the local symbols for marmot-pcln, which drops
			// them while it writes the table (as -x does on a normal link);
			// they were most of a dev binary (47.6 of 196 MB of tuios), and
			// names come from the pc table. strip -x when it could not.
			if !keep && !droppedRe.MatchString(b.lastOutput) {
				if err := b.run("xcrun", "strip", "-x", tmp); err != nil {
					return err
				}
			}
			return os.Rename(tmp, out)
		}
		m := needsRe.FindStringSubmatch(err.Error())
		if m == nil {
			return err
		}
		n, _ := strconv.Atoi(m[1])
		reserve = n + n/10 + 4096
	}
	return fmt.Errorf("placeholder still too small")
}
