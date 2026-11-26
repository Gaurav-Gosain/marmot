package driver

import (
	"fmt"
	"os"
)

// pieArgs are the ELF linker arguments for the kind of executable.
//
// A position independent executable has a relative relocation for every
// address in its data (tuios: 556k entries, a 13 MB .rela.dyn that ld.so
// reads at startup, and all of .data written while relocating). A position
// dependent one (-no-pie, as go build makes on linux) resolves them at link
// time; MARMOT_NOPIE=1 asks for it, for now (the default once measured).
// The objects stay position independent; the linker relaxes their GOT
// references. -buildmode=pie keeps a PIE with packed relative relocations
// (RELR: the table shrinks to a few KB; .data is still written at startup).
func (b *build) pieArgs() []string {
	if b.cfg.Static {
		return nil // static executables are position dependent already
	}
	if b.cfg.Race {
		return []string{"-no-pie"} // TSan's Go mapping wants globals low (race.go)
	}
	switch b.cfg.BuildMode {
	case "pie":
		return []string{"-pie", "-Wl,-z,pack-relative-relocs"}
	}
	if os.Getenv("MARMOT_NOPIE") == "1" {
		return []string{"-no-pie"}
	}
	return nil
}

// checkBuildMode rejects an unknown -buildmode.
func checkBuildMode(m string) error {
	switch m {
	case "", "exe", "pie":
		return nil
	}
	return fmt.Errorf("-buildmode %s: only exe and pie", m)
}
