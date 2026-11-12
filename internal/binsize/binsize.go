// Package binsize reads an executable and attributes its size to sections,
// packages and functions. For marmot binaries the function names and
// extents come from marmot's pc table (the binaries keep no local
// symbols); for other binaries from the symbol table.
package binsize

import (
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Func is one function of the text section.
type Func struct {
	Name  string `json:"name"`
	Pkg   string `json:"package"`
	Start uint64 `json:"-"` // offset from the image base
	Size  uint64 `json:"size"`
}

// Section is one section of the file.
type Section struct {
	Name string `json:"name"`
	Size uint64 `json:"size"`
}

// Binary is what Open found.
type Binary struct {
	Path     string    `json:"path"`
	FileSize int64     `json:"fileSize"`
	Format   string    `json:"format"` // macho or elf
	Arch     string    `json:"arch"`
	Sections []Section `json:"sections"`
	Funcs    []Func    `json:"-"`
	Source   string    `json:"source"` // pcln or symtab
	text     []byte
	textAddr uint64 // offset of the text section from the image base
}

// Open reads the binary at path.
func Open(path string) (*Binary, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	b := &Binary{Path: path, FileSize: st.Size()}
	if f, err := macho.Open(path); err == nil {
		defer f.Close()
		return b, b.readMacho(f)
	}
	if f, err := elf.Open(path); err == nil {
		defer f.Close()
		return b, b.readELF(f)
	}
	return nil, fmt.Errorf("%s: not a Mach-O or ELF executable", path)
}

func (b *Binary) readMacho(f *macho.File) error {
	b.Format = "macho"
	b.Arch = map[macho.Cpu]string{macho.CpuArm64: "arm64", macho.CpuAmd64: "amd64"}[f.Cpu]
	var base uint64
	for _, l := range f.Loads {
		if s, ok := l.(*macho.Segment); ok && s.Name == "__TEXT" {
			base = s.Addr
		}
	}
	var pcln []byte
	for _, s := range f.Sections {
		b.Sections = append(b.Sections, Section{Name: s.Seg + "," + s.Name, Size: s.Size})
		switch {
		case s.Seg == "__TEXT" && s.Name == "__text":
			b.text, _ = s.Data()
			b.textAddr = s.Addr - base
		case strings.Contains(s.Name, "pcln") && (s.Seg == "__TEXT" || s.Seg == "__MARMOT"):
			pcln, _ = s.Data()
		}
	}
	if pcln != nil && b.readPcln(pcln) == nil {
		b.Source = "pcln"
		return nil
	}
	if f.Symtab == nil {
		return errors.New("no pc table and no symbols (built with -s?)")
	}
	var syms []symbol
	for _, s := range f.Symtab.Syms {
		if s.Sect > 0 && int(s.Sect) <= len(f.Sections) && f.Sections[s.Sect-1].Name == "__text" && s.Type&0x0e == 0x0e {
			syms = append(syms, symbol{strings.TrimPrefix(s.Name, "_"), s.Value - base})
		}
	}
	return b.fromSymbols(syms, b.textAddr+uint64(len(b.text)))
}

func (b *Binary) readELF(f *elf.File) error {
	b.Format = "elf"
	b.Arch = map[elf.Machine]string{elf.EM_AARCH64: "arm64", elf.EM_X86_64: "amd64"}[f.Machine]
	var base uint64 = 1 << 63
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD && p.Vaddr < base {
			base = p.Vaddr
		}
	}
	var pcln []byte
	for _, s := range f.Sections {
		if s.Name == "" {
			continue
		}
		b.Sections = append(b.Sections, Section{Name: s.Name, Size: s.Size})
		switch s.Name {
		case ".text":
			b.text, _ = s.Data()
			b.textAddr = s.Addr - base
		case ".marmot_pcln":
			pcln, _ = s.Data()
		}
	}
	if pcln != nil && b.readPcln(pcln) == nil {
		b.Source = "pcln"
		return nil
	}
	syms, err := f.Symbols()
	if err != nil {
		return errors.New("no pc table and no symbols (built with -s?)")
	}
	var ss []symbol
	for _, s := range syms {
		if elf.ST_TYPE(s.Info) == elf.STT_FUNC && s.Value != 0 {
			ss = append(ss, symbol{s.Name, s.Value - base})
		}
	}
	return b.fromSymbols(ss, b.textAddr+uint64(len(b.text)))
}

type symbol struct {
	name string
	off  uint64
}

func (b *Binary) fromSymbols(syms []symbol, end uint64) error {
	b.Source = "symtab"
	sort.Slice(syms, func(i, j int) bool { return syms[i].off < syms[j].off })
	for i, s := range syms {
		next := end
		if i+1 < len(syms) {
			next = syms[i+1].off
		}
		if next <= s.off {
			continue
		}
		b.Funcs = append(b.Funcs, Func{Name: s.name, Pkg: PackageOf(s.name), Start: s.off, Size: next - s.off})
	}
	return nil
}

// readPcln decodes the function list of a marmot pc table (versions 2 and
// 3): start offsets and names; sizes are the distance to the next start.
func (b *Binary) readPcln(t []byte) error {
	const hdr = 8 + 13*4
	if len(t) < hdr || string(t[:8]) != "MRMTPCLN" {
		return errors.New("not a pc table")
	}
	u32 := func(off uint32) uint32 {
		if int(off)+4 > len(t) {
			return 0
		}
		return binary.LittleEndian.Uint32(t[off:])
	}
	if v := u32(8); v != 2 && v != 3 {
		return fmt.Errorf("pc table version %d", v)
	}
	nfuncs, npkgs := u32(12), u32(24)
	textEnd, funcOff, pkgOff, strOff := u32(32), u32(36), u32(48), u32(56)
	cstr := func(off uint32) string {
		i := strOff + off
		j := i
		for int(j) < len(t) && t[j] != 0 {
			j++
		}
		if int(i) > len(t) {
			return ""
		}
		return string(t[i:j])
	}
	record := func(off uint32) string {
		// The prefix index is read from the raw bytes (as the runtime
		// does); the rest follows it up to the NUL.
		i := int(strOff + off)
		if i >= len(t) {
			return ""
		}
		k, n := binary.Uvarint(t[i:])
		if n <= 0 {
			return cstr(off)
		}
		rest := cstr(off + uint32(n))
		if k == 0 || uint32(k) > npkgs {
			return rest
		}
		return cstr(u32(pkgOff+4*(uint32(k)-1))) + rest
	}
	for k := uint32(0); k < nfuncs; k++ {
		start := u32(funcOff + 12*k)
		end := textEnd
		if k+1 < nfuncs {
			end = u32(funcOff + 12*(k+1))
		}
		name := record(u32(funcOff + 12*k + 4))
		if end > start {
			b.Funcs = append(b.Funcs, Func{Name: name, Pkg: PackageOf(name), Start: uint64(start), Size: uint64(end - start)})
		}
	}
	return nil
}

// PackageOf returns the package of a function name: the path before the
// last dot of the first element after the last slash (pkg.F, pkg.(*T).M,
// a/b.T.M), "C" for names without a package (C and assembly runtime code).
func PackageOf(name string) string {
	slash := strings.LastIndexByte(name, '/')
	rest := name[slash+1:]
	i := strings.IndexByte(rest, '.')
	if i < 0 {
		return "C"
	}
	return name[:slash+1+i]
}

// PackageSizes sums function sizes by package, largest first.
func (b *Binary) PackageSizes() []Section {
	m := map[string]uint64{}
	for _, f := range b.Funcs {
		m[f.Pkg] += f.Size
	}
	out := make([]Section, 0, len(m))
	for k, v := range m {
		out = append(out, Section{Name: k, Size: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Size != out[j].Size {
			return out[i].Size > out[j].Size
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// TextSize is the size of the text section.
func (b *Binary) TextSize() uint64 { return uint64(len(b.text)) }

// funcAt returns the index of the function holding offset off.
func (b *Binary) funcAt(off uint64) int {
	i := sort.Search(len(b.Funcs), func(i int) bool { return b.Funcs[i].Start > off }) - 1
	if i < 0 || off >= b.Funcs[i].Start+b.Funcs[i].Size {
		return -1
	}
	return i
}

// Callers returns, for every function, the functions that call it
// directly (arm64 BL and B, amd64 CALL rel32). Calls through function
// values and interfaces are not seen.
func (b *Binary) Callers() map[int][]int {
	callers := map[int]map[int]bool{}
	add := func(from, target uint64) {
		ti := b.funcAt(target)
		fi := b.funcAt(from)
		if ti < 0 || fi < 0 || ti == fi || b.Funcs[ti].Start != target {
			return
		}
		if callers[ti] == nil {
			callers[ti] = map[int]bool{}
		}
		callers[ti][fi] = true
	}
	switch b.Arch {
	case "arm64":
		for i := 0; i+4 <= len(b.text); i += 4 {
			ins := binary.LittleEndian.Uint32(b.text[i:])
			if ins&0x7c000000 == 0x14000000 { // B or BL imm26
				imm := int64(ins & 0x03ffffff)
				if imm&0x02000000 != 0 {
					imm -= 1 << 26
				}
				pc := b.textAddr + uint64(i)
				add(pc, uint64(int64(pc)+imm*4))
			}
		}
	case "amd64":
		for i := 0; i+5 <= len(b.text); i++ {
			if b.text[i] == 0xe8 {
				rel := int64(int32(binary.LittleEndian.Uint32(b.text[i+1:])))
				pc := b.textAddr + uint64(i)
				add(pc, uint64(int64(pc)+5+rel))
			}
		}
	}
	out := map[int][]int{}
	for t, fs := range callers {
		for f := range fs {
			out[t] = append(out[t], f)
		}
		sort.Ints(out[t])
	}
	return out
}

// Why returns a shortest chain of direct calls from a root (main.main,
// marmot_main, a package init) to the function named name, innermost
// last, or nil when the function is only reached indirectly.
func (b *Binary) Why(name string) ([]string, bool) {
	target := -1
	for i, f := range b.Funcs {
		if f.Name == name {
			target = i
		}
	}
	if target < 0 {
		return nil, false
	}
	callers := b.Callers()
	isRoot := func(n string) bool {
		return n == "main.main" || n == "marmot_main" || strings.HasSuffix(n, ".init") || n == "main"
	}
	prev := map[int]int{target: -1}
	queue := []int{target}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if isRoot(b.Funcs[cur].Name) {
			var chain []string
			for k := cur; k != -1; k = prev[k] {
				chain = append(chain, b.Funcs[k].Name)
			}
			return chain, true
		}
		for _, c := range callers[cur] {
			if _, seen := prev[c]; !seen {
				prev[c] = cur
				queue = append(queue, c)
			}
		}
	}
	// No path from a root: list the direct callers.
	var chain []string
	for _, c := range callers[target] {
		chain = append(chain, b.Funcs[c].Name)
	}
	return chain, true
}
