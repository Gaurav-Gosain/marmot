package driver

import (
	"debug/macho"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// The pc table (docs/pcln.md): after the link, build the table from the
// DWARF line tables the backend emitted for the frontend's locations and
// put it in the binary (Mach-O: the end of __TEXT, so a small table
// fits in the segment's free tail). -s (Config.Strip) or MARMOT_STRIP=1 skips it.

func (b *build) wantPcln() bool {
	if b.cfg.Strip || os.Getenv("MARMOT_STRIP") != "" || os.Getenv("MARMOT_NO_LOCS") != "" || b.cfg.Emit != "exe" || b.cross() {
		return false
	}
	return fileExists(b.pclnTool())
}

// pclnV3 turns on version 3 tables (inlined frames) for dev builds and
// -release -trace=full, once the runtime reads them (rtgo).
const pclnV3 = true

// pclnArgs are the table builder's arguments before the inputs.
// MARMOT_PCLN_VERSION overrides the version.
func (b *build) pclnArgs(tbl string) []string {
	a := []string{"-o", tbl}
	v := os.Getenv("MARMOT_PCLN_VERSION")
	if v == "" && pclnV3 && (!b.cfg.Release || b.cfg.Trace == "full") {
		v = "3"
	}
	if v != "" {
		a = append(a, "-table-version="+v)
	}
	return a
}

func (b *build) pclnTool() string {
	return filepath.Join(b.cfg.Root, "build", "bin", "marmot-pcln")
}

// linkWithPcln links out with the table. link(args) runs the linker with
// the given extra arguments and output.
func (b *build) linkWithPcln(link func(extra []string, out string) error, out string) error {
	tmp := filepath.Join(b.work, "nopcln.exe")
	if runtime.GOOS != "darwin" {
		// ELF: the DWARF is in the binary.
		if err := link(nil, tmp); err != nil {
			return err
		}
		tbl := filepath.Join(b.work, "pcln.bin")
		if err := b.run(b.pclnTool(), append(b.pclnArgs(tbl), tmp)...); err != nil {
			b.logf("pcln: %v (no table)", err)
			return b.run(llvmTool("llvm-objcopy"), "--strip-all", tmp, out)
		}
		if err := b.run(llvmTool("llvm-objcopy"), "--strip-all", tmp, tmp+".s"); err != nil {
			return err
		}
		return b.run(llvmTool("llvm-objcopy"), "--add-section", ".marmot_pcln="+tbl, tmp+".s", out)
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

// sameText reports whether two Mach-O files have their code at the same
// addresses (the table describes the first file's __text).
func sameText(a, b string) (bool, error) {
	fa, err := macho.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := macho.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()
	sa, sb := fa.Section("__text"), fb.Section("__text")
	if sa == nil || sb == nil {
		return false, fmt.Errorf("no __text")
	}
	return sa.Addr == sb.Addr && sa.Size == sb.Size, nil
}
