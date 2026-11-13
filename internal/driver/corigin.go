package driver

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// The C origin table says which C library each range of the binary's code
// comes from, for marmot size: a cgo package's C files, the runtime core,
// a static library of #cgo LDFLAGS, or the static C library. Function names
// map to an origin by the inputs that define them (llvm-nm); the binary's
// symbols, read before they are stripped, give the ranges.
//
// Encoding (little endian), at the end of the pc table section on Mach-O
// and in a section .marmot_corigin on ELF:
//
//	u32 nlabels, then per label u16 length and the bytes
//	u32 nranges, then per range u64 start, u64 end (offsets from the image
//	base, as the pc table's), u16 label
//	u32 length of all the above, "MCOR"
//
// Only builds with C beyond the runtime core get one.

const cOriginMagic = "MCOR"

// cOriginNames maps C function names to their origin labels ("" for a
// name several origins define).
func (b *build) cOriginNames(rt string) map[string]string {
	if len(b.cgoObjPkg) == 0 && len(b.staticCLibs()) == 0 {
		return nil
	}
	names := map[string]string{}
	add := func(file, label string) {
		for _, n := range definedText(file, b.goos == "darwin") {
			if old, ok := names[n]; ok && old != label {
				names[n] = ""
				continue
			}
			names[n] = label
		}
	}
	objs := make([]string, 0, len(b.cgoObjPkg))
	for o := range b.cgoObjPkg {
		objs = append(objs, o)
	}
	sort.Strings(objs)
	for _, o := range objs {
		add(o, b.cgoObjPkg[o]+" (C)")
	}
	add(rt, "runtime core (C)")
	for _, l := range b.staticCLibs() {
		add(l.path, l.label)
	}
	return names
}

type staticLib struct{ path, label string }

// staticCLibs are the static archives the link takes C code from: those of
// #cgo LDFLAGS (an archive path, or -lNAME found as libNAME.a with no shared
// library beside it), and for -static the C library.
func (b *build) staticCLibs() []staticLib {
	var out []staticLib
	var dirs, names []string
	for i := 0; i < len(b.cgoLDFLAGS); i++ {
		f := b.cgoLDFLAGS[i]
		switch {
		case f == "-L" && i+1 < len(b.cgoLDFLAGS):
			dirs = append(dirs, b.cgoLDFLAGS[i+1])
			i++
		case strings.HasPrefix(f, "-L"):
			dirs = append(dirs, f[2:])
		case f == "-l" && i+1 < len(b.cgoLDFLAGS):
			names = append(names, b.cgoLDFLAGS[i+1])
			i++
		case strings.HasPrefix(f, "-l"):
			names = append(names, f[2:])
		case strings.HasSuffix(f, ".a") && fileExists(f):
			out = append(out, staticLib{f, filepath.Base(f)})
		}
	}
	dirs = append(dirs, "/usr/local/lib", "/opt/homebrew/lib", "/usr/lib")
	if sr := b.crossSysroot(); sr != "" {
		dirs = append(dirs, filepath.Join(sr, "usr", "lib"))
	}
	for _, n := range names {
		for _, d := range dirs {
			a := filepath.Join(d, "lib"+n+".a")
			if !fileExists(a) {
				continue
			}
			shared := false
			for _, ext := range []string{".dylib", ".so", ".tbd"} {
				if fileExists(filepath.Join(d, "lib"+n+ext)) {
					shared = true
				}
			}
			if !shared {
				out = append(out, staticLib{a, "lib" + n + ".a"})
			}
			break
		}
	}
	if b.cfg.Static {
		if sr := b.crossSysroot(); sr != "" {
			out = append(out, staticLib{filepath.Join(sr, "usr", "lib", "libc.a"), "libc (musl)"})
		}
		if g := b.gccDir(b.crossSysroot()); g != "" {
			out = append(out, staticLib{filepath.Join(g, "libgcc.a"), "libgcc"})
		}
	}
	return out
}

// definedText lists the function symbols a file (object, archive or
// bitcode) defines, without Mach-O's leading underscore (macho).
func definedText(file string, macho bool) []string {
	out, err := exec.Command(llvmTool("llvm-nm"), "--defined-only", "--no-sort", "-P", file).Output()
	if err != nil {
		return nil
	}
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 || (f[1] != "T" && f[1] != "t" && f[1] != "W") { // W: weak aliases (musl)
			continue
		}
		n := f[0]
		if macho {
			n = strings.TrimPrefix(n, "_")
		}
		names = append(names, n)
	}
	return names
}

type textSym struct {
	name string
	off  uint64
}

// binaryText reads the function symbols of a linked binary, as offsets
// from the image base, and the end of its text.
func binaryText(bin string) ([]textSym, uint64, bool) {
	if f, err := macho.Open(bin); err == nil {
		defer func() { _ = f.Close() }() // read only
		var base uint64
		for _, l := range f.Loads {
			if s, ok := l.(*macho.Segment); ok && s.Name == "__TEXT" {
				base = s.Addr
			}
		}
		text := f.Section("__text")
		if text == nil || f.Symtab == nil {
			return nil, 0, false
		}
		var syms []textSym
		for _, s := range f.Symtab.Syms {
			if s.Sect > 0 && int(s.Sect) <= len(f.Sections) && f.Sections[s.Sect-1] == text && s.Type&0x0e == 0x0e {
				syms = append(syms, textSym{strings.TrimPrefix(s.Name, "_"), s.Value - base})
			}
		}
		return syms, text.Addr + text.Size - base, true
	}
	f, err := elf.Open(bin)
	if err != nil {
		return nil, 0, false
	}
	defer func() { _ = f.Close() }() // read only
	var base uint64 = 1 << 63
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD && p.Vaddr < base {
			base = p.Vaddr
		}
	}
	text := f.Section(".text")
	all, err := f.Symbols()
	if text == nil || err != nil {
		return nil, 0, false
	}
	var syms []textSym
	for _, s := range all {
		if elf.ST_TYPE(s.Info) == elf.STT_FUNC && s.Value != 0 {
			syms = append(syms, textSym{s.Name, s.Value - base})
		}
	}
	return syms, text.Addr + text.Size - base, true
}

// cOriginTable builds the table of bin, or nil.
func (b *build) cOriginTable(bin, rt string) []byte {
	names := b.cOriginNames(rt)
	if names == nil {
		return nil
	}
	syms, end, ok := binaryText(bin)
	if !ok {
		return nil
	}
	sort.SliceStable(syms, func(i, j int) bool { return syms[i].off < syms[j].off })
	var labels []string
	index := map[string]uint16{}
	type rng struct {
		start, end uint64
		label      uint16
	}
	var rs []rng
	for i := 0; i < len(syms); {
		// The names at one address (aliases): the first with an origin.
		s, l := syms[i], ""
		j := i
		for ; j < len(syms) && syms[j].off == s.off; j++ {
			if l == "" {
				l = names[syms[j].name]
			}
		}
		i = j
		next := end
		if j < len(syms) {
			next = syms[j].off
		}
		if l == "" || next <= s.off {
			continue
		}
		k, ok := index[l]
		if !ok {
			k = uint16(len(labels))
			index[l] = k
			labels = append(labels, l)
		}
		if n := len(rs); n > 0 && rs[n-1].label == k && rs[n-1].end == s.off {
			rs[n-1].end = next
			continue
		}
		rs = append(rs, rng{s.off, next, k})
	}
	if len(rs) == 0 {
		return nil
	}
	var buf bytes.Buffer
	w := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	w(uint32(len(labels)))
	for _, l := range labels {
		w(uint16(len(l)))
		buf.WriteString(l)
	}
	w(uint32(len(rs)))
	for _, r := range rs {
		w(r.start)
		w(r.end)
		w(r.label)
	}
	w(uint32(buf.Len()))
	buf.WriteString(cOriginMagic)
	return buf.Bytes()
}

// cOriginSectionArgs writes the table of bin to a file and returns the
// llvm-objcopy flags that add it as a section, or nil.
func (b *build) cOriginSectionArgs(bin, rt string) []string {
	t := b.cOriginTable(bin, rt)
	if t == nil {
		return nil
	}
	f := filepath.Join(b.work, "corigin.bin")
	if err := os.WriteFile(f, t, 0o644); err != nil {
		return nil
	}
	return []string{"--add-section", ".marmot_corigin=" + f}
}

// patchCOriginMacho writes the table of bin at the end of its pc table
// section (after the table, which is used bytes long), if it fits, and
// rehashes the changed pages in the ad hoc code signature, as marmot-pcln's
// patch does. The result is a new file (the kernel caches signatures per
// file).
func (b *build) patchCOriginMacho(bin, rt string, used int) {
	t := b.cOriginTable(bin, rt)
	if t == nil {
		return
	}
	if err := patchMachoTail(bin, t, used); err != nil {
		b.logf("C origins: %v", err)
	}
}

func patchMachoTail(bin string, t []byte, used int) error {
	f, err := macho.Open(bin)
	if err != nil {
		return err
	}
	s := f.Section("__marmot_pcln")
	var sigOff, sigSize uint32
	for _, l := range f.Loads {
		if raw, ok := l.(macho.LoadBytes); ok && len(raw) >= 16 && binary.LittleEndian.Uint32(raw) == 0x1d {
			sigOff, sigSize = binary.LittleEndian.Uint32(raw[8:]), binary.LittleEndian.Uint32(raw[12:])
		}
	}
	_ = f.Close()
	if s == nil || used <= 0 || uint64(used+len(t)) > s.Size {
		return fmt.Errorf("%d bytes do not fit after the pc table", len(t))
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		return err
	}
	at := int(s.Offset) + int(s.Size) - len(t)
	if at+len(t) > len(data) || int(sigOff)+int(sigSize) > len(data) {
		return fmt.Errorf("bad section or signature offset")
	}
	copy(data[at:], t)
	if sigSize > 0 {
		sig := data[sigOff : sigOff+sigSize]
		be := binary.BigEndian
		if len(sig) < 12 || be.Uint32(sig) != 0xfade0cc0 {
			return fmt.Errorf("unknown code signature")
		}
		for k := range be.Uint32(sig[8:]) {
			if int(12+8*k+8) > len(sig) {
				break
			}
			cd := sig[be.Uint32(sig[12+8*k+4:]):]
			if len(cd) < 40 || be.Uint32(cd) != 0xfade0c02 {
				continue
			}
			hashOff, nSlots, limit := be.Uint32(cd[16:]), be.Uint32(cd[28:]), be.Uint32(cd[32:])
			hashSize, hashType, shift := int(cd[36]), cd[37], cd[39]
			if hashType != 2 {
				return fmt.Errorf("unsupported code signature hash %d", hashType)
			}
			page := 4096
			if shift != 0 {
				page = 1 << shift
			}
			for p := at / page; p <= (at+len(t)-1)/page && p < int(nSlots); p++ {
				lo, hi := p*page, min(p*page+page, int(limit))
				h := sha256.Sum256(data[lo:hi])
				copy(cd[int(hashOff)+p*hashSize:], h[:hashSize])
			}
		}
	}
	tmp := bin + ".corigin.tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, bin)
}
