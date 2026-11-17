package driver

import (
	"bytes"
	"os"
	"path/filepath"
)

// noptr reports whether codegen puts package variables without pointers in
// the sections the collector skips (codegen/noptr.go): on by default,
// MARMOT_NOPTR=0 turns it off. tier0 takes the sections too (41bd1170,
// a4091518).
func (b *build) noptr() bool {
	return os.Getenv("MARMOT_NOPTR") != "0"
}

// noptrScript gathers the pointer-free variables (ELF .data.marmot.noptr.*
// and .bss.marmot.noptr.*, one section per variable) into two output
// sections the runtime finds by marmot_noptrdata_start and _end and
// marmot_noptrbss_start and _end. Without it they would land in .data and
// .bss among the variables it must scan. INSERT keeps the default script;
// BEFORE, or the default *(.data.*) and *(.bss.*) take them first.
const noptrScript = `SECTIONS
{
  .marmot_noptrdata : ALIGN(64)
  {
    marmot_noptrdata_start = .;
    *(.data.marmot.noptr.*)
    marmot_noptrdata_end = .;
  }
}
INSERT BEFORE .data;
SECTIONS
{
  .marmot_noptrbss (NOLOAD) : ALIGN(64)
  {
    marmot_noptrbss_start = .;
    *(.bss.marmot.noptr.*)
    marmot_noptrbss_end = .;
  }
}
INSERT BEFORE .bss;
`

// noptrLinkArgs returns the linker arguments for noptrScript on an ELF link,
// writing the script under build/cache once. mold takes no SECTIONS: there
// the variables stay in .bss and are scanned as before.
func (b *build) noptrLinkArgs() []string {
	if !b.noptr() || os.Getenv("MARMOT_LD") == "mold" {
		return nil
	}
	p := filepath.Join(b.cfg.Root, "build", "cache", "noptr.ld")
	if old, err := os.ReadFile(p); err != nil || !bytes.Equal(old, []byte(noptrScript)) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil
		}
		tmp := p + ".tmp" + filepath.Base(b.work)
		if os.WriteFile(tmp, []byte(noptrScript), 0o644) != nil || os.Rename(tmp, p) != nil {
			os.Remove(tmp)
			return nil
		}
	}
	return []string{"-Wl,-T," + p}
}
