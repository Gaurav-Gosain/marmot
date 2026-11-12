package main

// gcbin.go: type descriptors in the linked binary against the MLIR they were translated from
// (precise GC contract). -gcdata and -gcsites check the MLIR; this checks
// that the backend and the linker carried each descriptor through: the size and ptrdata words
// the runtime reads, the bitmap bytes, and that the gcdata word points at the bitmap. A wrong
// data layout during translation (struct fields read at the wrong offset) shows here.
// Input: a marmot -work directory (m*.mlir and nopcln.exe, the linked program before the
// pcln table is appended, which still has its symbols). Mach-O arm64 only.

import (
	"debug/macho"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type binResult struct {
	checked, missing int
	bad              []string
}

func checkGCBin(workDir string) (*binResult, error) {
	mlirs, err := filepath.Glob(filepath.Join(workDir, "m*.mlir"))
	if err != nil {
		return nil, err
	}
	descs := map[string]*tdesc{}
	for _, m := range mlirs {
		d, err := readDescs(m)
		if err != nil {
			return nil, err
		}
		for k, v := range d {
			descs[k] = v
		}
	}
	f, err := macho.Open(filepath.Join(workDir, "nopcln.exe"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if f.Symtab == nil {
		return nil, fmt.Errorf("no symbol table")
	}
	syms := map[string]uint64{}
	for _, s := range f.Symtab.Syms {
		if s.Sect != 0 && s.Type&0xe0 == 0 { // defined in a section, not a debug entry
			syms[strings.TrimPrefix(s.Name, "_")] = s.Value
		}
	}
	var base uint64
	if seg := f.Segment("__TEXT"); seg != nil {
		base = seg.Addr
	}
	read := func(addr uint64, n int) ([]byte, error) {
		for _, sec := range f.Sections {
			if addr >= sec.Addr && addr+uint64(n) <= sec.Addr+sec.Size {
				if sec.Flags&0xff == 0x1 { // S_ZEROFILL
					return make([]byte, n), nil
				}
				b := make([]byte, n)
				_, err := sec.ReadAt(b, int64(addr-sec.Addr))
				return b, err
			}
		}
		return nil, fmt.Errorf("address %#x in no section", addr)
	}
	res := &binResult{}
	names := make([]string, 0, len(descs))
	for n := range descs {
		names = append(names, n)
	}
	sort.Strings(names)
	const mask36 = 1<<36 - 1
	for _, n := range names {
		d := descs[n]
		addr, ok := syms["type:"+n]
		if !ok {
			res.missing++ // dropped as unused by the linker
			continue
		}
		res.checked++
		hdr, err := read(addr, 24)
		if err != nil {
			res.bad = append(res.bad, n+": "+err.Error())
			continue
		}
		var problems []string
		size := int64(binary.LittleEndian.Uint64(hdr[0:]))
		ptrdata := int64(binary.LittleEndian.Uint64(hdr[8:]))
		raw := binary.LittleEndian.Uint64(hdr[16:])
		if size != d.size {
			problems = append(problems, fmt.Sprintf("size word %d, MLIR %d", size, d.size))
		}
		if ptrdata != d.ptrdata {
			problems = append(problems, fmt.Sprintf("ptrdata word %d, MLIR %d", ptrdata, d.ptrdata))
		}
		if d.hasBits {
			ga, ok := syms["type:"+n+".gcdata"]
			if !ok {
				problems = append(problems, "no gcdata symbol")
			} else {
				// a chained-fixup rebase keeps the target in the low 36 bits, as a vmaddr or an
				// offset from the image base
				t := raw & mask36
				if raw>>63 != 0 || (t != ga&mask36 && t+base != ga) {
					problems = append(problems, fmt.Sprintf("gcdata word %#x does not point at the bitmap %#x", raw, ga))
				}
				b, err := read(ga, len(d.bits))
				if err != nil {
					problems = append(problems, err.Error())
				} else if string(b) != string(d.bits) {
					problems = append(problems, fmt.Sprintf("bitmap bytes %x, MLIR %x", b, d.bits))
				}
			}
		} else if raw != 0 {
			problems = append(problems, fmt.Sprintf("gcdata word %#x for a descriptor without a bitmap", raw))
		}
		if len(problems) > 0 {
			res.bad = append(res.bad, n+": "+strings.Join(problems, "; "))
		}
	}
	return res, nil
}
