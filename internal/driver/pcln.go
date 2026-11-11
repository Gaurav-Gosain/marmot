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
// put it in the binary. -s (Config.Strip) or MARMOT_STRIP=1 skips it.

func (b *build) wantPcln() bool {
	if b.cfg.Strip || os.Getenv("MARMOT_STRIP") != "" || os.Getenv("MARMOT_NO_LOCS") != "" || b.cfg.Emit != "exe" || b.cross() {
		return false
	}
	return fileExists(b.pclnTool())
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
		if err := b.run(b.pclnTool(), "-o", tbl, tmp); err != nil {
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
	if err := link([]string{"-Wl,-sectcreate,__MARMOT,__pcln," + ph}, tmp); err != nil {
		return err
	}
	dsym := filepath.Join(b.work, "nopcln.dSYM")
	tbl := filepath.Join(b.work, "pcln.bin")
	if err := b.run("xcrun", "dsymutil", tmp, "-o", dsym); err != nil {
		b.logf("pcln: dsymutil: %v (no table)", err)
		return os.Rename(tmp, out)
	}
	dwarf := filepath.Join(dsym, "Contents", "Resources", "DWARF", filepath.Base(tmp))
	if err := b.run(b.pclnTool(), "-o", tbl, "-base-from", tmp, dwarf); err != nil {
		b.logf("pcln: %v (no table)", err)
		return os.Rename(tmp, out)
	}
	if err := link([]string{"-Wl,-sectcreate,__MARMOT,__pcln," + tbl, "-Wl,-S"}, out); err != nil {
		return err
	}
	if same, err := sameText(tmp, out); err != nil || !same {
		b.logf("pcln: code moved in the relink (%v); no table", err)
		return os.Rename(tmp, out)
	}
	b.logf("pcln table added")
	return nil
}

// sameText reports whether two Mach-O files have the same __TEXT layout.
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
	sa, sb := fa.Segment("__TEXT"), fb.Segment("__TEXT")
	if sa == nil || sb == nil {
		return false, fmt.Errorf("no __TEXT")
	}
	if sa.Addr != sb.Addr || sa.Memsz != sb.Memsz {
		return false, nil
	}
	for _, s := range fa.Sections {
		if s.Seg != "__TEXT" {
			continue
		}
		t := fb.Section(s.Name)
		if t == nil || t.Addr != s.Addr || t.Size != s.Size {
			return false, nil
		}
	}
	return true, nil
}
