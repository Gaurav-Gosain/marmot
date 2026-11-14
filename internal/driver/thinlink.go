package driver

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The thin link's reference graph (docs: optimized builds, link time
// liveness). A program's objects are read for their symbols and relocations
// only: every defined symbol is a node, and a relocation in the section (ELF,
// function and data sections) or the address range (Mach-O, subsections by
// symbol) of a symbol is an edge to the symbol it names. Liveness marks from
// the roots the system linker keeps (the entry point, init arrays, retained
// sections). The method rule of the thin link runs on top of this graph:
// method arrays are defined by the thin link, not by package objects, so no
// edge reaches a method except through a live call site of its name.

// refGraph holds symbols by name and their outgoing references.
type refGraph struct {
	refs  map[string][]string // defined symbol -> symbols it references
	roots []string
	nobj  int // objects read: local symbols are named obj#name
	// Kept only when what they point at is live: Mach-O live support
	// sections and ELF SHF_LINK_ORDER sections (the type link entries).
	support map[string][]string
}

// localName names a symbol only its object sees (local labels and static
// functions: every object has an ltmp1).
func localName(obj int, name string) string { return strconv.Itoa(obj) + "#" + name }

func newRefGraph() *refGraph {
	return &refGraph{refs: map[string][]string{}, support: map[string][]string{}}
}

// addObject reads one object file, or every member of an archive (the
// runtime library: its C main and the Go functions it calls by name are
// roots of the program), into the graph.
func (g *refGraph) addObject(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.HasPrefix(data, []byte("!<arch>\n")) {
		return g.addArchive(path, data)
	}
	return g.addBytes(path, data)
}

func (g *refGraph) addBytes(name string, data []byte) error {
	g.nobj++
	if f, err := elf.NewFile(bytes.NewReader(data)); err == nil {
		return g.addELF(f)
	}
	if f, err := macho.NewFile(bytes.NewReader(data)); err == nil {
		return g.addMachO(f)
	}
	return fmt.Errorf("%s: not an ELF or Mach-O object", name)
}

// addArchive reads the object members of a System V or BSD ar archive.
func (g *refGraph) addArchive(path string, data []byte) error {
	var longNames []byte
	for off := 8; off+60 <= len(data); {
		h := data[off : off+60]
		name := strings.TrimRight(string(h[:16]), " ")
		size, err := strconv.Atoi(strings.TrimSpace(string(h[48:58])))
		if err != nil {
			return fmt.Errorf("%s: bad archive member header", path)
		}
		body := data[off+60 : min(off+60+size, len(data))]
		switch {
		case name == "/" || name == "/SYM64/" || strings.HasPrefix(name, "__.SYMDEF"):
		case name == "//":
			longNames = body
		case strings.HasPrefix(name, "#1/"): // BSD: the name precedes the data
			n, _ := strconv.Atoi(name[3:])
			if n <= len(body) {
				name, body = strings.TrimRight(string(body[:n]), "\x00"), body[n:]
			}
			fallthrough
		default:
			if strings.HasPrefix(name, "/") && longNames != nil {
				if i, err := strconv.Atoi(name[1:]); err == nil && i < len(longNames) {
					name = string(longNames[i:])
					name = name[:strings.IndexAny(name+"\n", "/\n")]
				}
			}
			if strings.HasPrefix(name, "__.SYMDEF") {
				break
			}
			if err := g.addBytes(path+"("+name+")", body); err != nil {
				return err
			}
		}
		off += 60 + size
		off += off & 1
	}
	return nil
}

func (g *refGraph) define(sym string) {
	if _, ok := g.refs[sym]; !ok {
		g.refs[sym] = nil
	}
}

func (g *refGraph) edge(from, to string) {
	if from != to {
		g.refs[from] = append(g.refs[from], to)
	}
}

// shfGNURetain is SHF_GNU_RETAIN (debug/elf does not name it).
const shfGNURetain = elf.SectionFlag(0x200000)

// elfRetained reports sections the linker keeps whatever references them.
func elfRetained(s *elf.Section) bool {
	n := s.Name
	return s.Flags&shfGNURetain != 0 || strings.HasPrefix(n, ".init_array") ||
		strings.HasPrefix(n, ".fini_array") || strings.HasPrefix(n, ".preinit_array") ||
		strings.HasPrefix(n, ".ctors") || strings.HasPrefix(n, ".dtors") || n == ".init" || n == ".fini"
}

func (g *refGraph) addELF(f *elf.File) error {
	syms, err := f.Symbols()
	if err != nil {
		return err
	}
	// Symbols by section: the nodes a relocation in that section starts from.
	// A section without a named symbol (a section symbol only) gets a node of
	// its own, named after the section and the object, so references
	// through it are followed.
	bySection := map[elf.SectionIndex][]string{}
	obj := g.nobj
	secNode := func(i elf.SectionIndex) string {
		if names := bySection[i]; len(names) > 0 {
			return names[0]
		}
		return localName(obj, "section:"+f.Sections[i].Name+":"+strconv.Itoa(int(i)))
	}
	symName := func(s elf.Symbol) string {
		if elf.ST_BIND(s.Info) == elf.STB_LOCAL {
			return localName(obj, s.Name)
		}
		return s.Name
	}
	for _, s := range syms {
		if s.Section == elf.SHN_UNDEF || s.Section >= elf.SHN_LORESERVE || s.Name == "" {
			continue
		}
		t := elf.ST_TYPE(s.Info)
		if t != elf.STT_FUNC && t != elf.STT_OBJECT && t != elf.STT_NOTYPE && t != elf.STT_TLS {
			continue
		}
		bySection[s.Section] = append(bySection[s.Section], symName(s))
		g.define(symName(s))
	}
	// Symbols of one section reference each other (a section is kept or
	// dropped whole).
	for _, names := range bySection {
		for _, a := range names[1:] {
			g.edge(names[0], a)
			g.edge(a, names[0])
		}
	}
	for i, s := range f.Sections {
		if s.Flags&elf.SHF_LINK_ORDER != 0 && int(s.Link) < len(f.Sections) && s.Link != 0 {
			n := secNode(elf.SectionIndex(i))
			g.define(n)
			g.support[n] = append(g.support[n], secNode(elf.SectionIndex(s.Link)))
			continue
		}
		if elfRetained(s) {
			g.roots = append(g.roots, secNode(elf.SectionIndex(i)))
			g.define(secNode(elf.SectionIndex(i)))
		}
	}
	for _, rs := range f.Sections {
		if rs.Type != elf.SHT_RELA || int(rs.Info) >= len(f.Sections) {
			continue
		}
		target := elf.SectionIndex(rs.Info)
		from := secNode(target)
		g.define(from)
		data, err := rs.Data()
		if err != nil {
			return err
		}
		for off := 0; off+24 <= len(data); off += 24 {
			info := binary.LittleEndian.Uint64(data[off+8:])
			idx := int(info >> 32)
			if idx == 0 || idx > len(syms) {
				continue
			}
			s := syms[idx-1]
			switch {
			case s.Name != "" && elf.ST_TYPE(s.Info) != elf.STT_SECTION:
				g.edge(from, symName(s))
			case s.Section != elf.SHN_UNDEF && s.Section < elf.SHN_LORESERVE:
				g.edge(from, secNode(s.Section))
			}
		}
	}
	return nil
}

func (g *refGraph) addMachO(f *macho.File) error {
	if f.Symtab == nil {
		return nil
	}
	// Defined symbols of each section, by address: a relocation belongs to
	// the symbol whose range holds its address (subsections by symbol).
	type at struct {
		addr uint64
		name string
	}
	bySect := map[int][]at{}
	obj := g.nobj
	symName := func(s macho.Symbol) string {
		n := strings.TrimPrefix(s.Name, "_")
		if s.Type&0x01 == 0 { // not N_EXT: this object's own
			return localName(obj, n)
		}
		return n
	}
	for _, s := range f.Symtab.Syms {
		if s.Sect == 0 || s.Type&0x0e != 0x0e || s.Name == "" { // N_SECT
			continue
		}
		name := symName(s)
		bySect[int(s.Sect)-1] = append(bySect[int(s.Sect)-1], at{s.Value, name})
		g.define(name)
		if s.Desc&0x20 != 0 { // N_NO_DEAD_STRIP
			g.roots = append(g.roots, name)
		}
	}
	for i := range bySect {
		// At one address a section's ltmp label sorts first, so a relocation
		// there belongs to the real atom (l_.mapc.852 beside ltmp4).
		sort.SliceStable(bySect[i], func(a, b int) bool {
			x, y := bySect[i][a], bySect[i][b]
			if x.addr != y.addr {
				return x.addr < y.addr
			}
			return isLtmp(x.name) && !isLtmp(y.name)
		})
		// Symbols at one address are one atom (aliases): joined both ways.
		// Not a section's ltmp start label, which only shares the address
		// of the first symbol.
		list := bySect[i]
		for j := 1; j < len(list); j++ {
			if list[j].addr == list[j-1].addr && !isLtmp(list[j].name) && !isLtmp(list[j-1].name) {
				g.edge(list[j].name, list[j-1].name)
				g.edge(list[j-1].name, list[j].name)
			}
		}
	}
	owner := func(sect int, addr uint64) string {
		list := bySect[sect]
		k := sort.Search(len(list), func(j int) bool { return list[j].addr > addr }) - 1
		if k < 0 {
			return ""
		}
		return list[k].name
	}
	defer func() {
		for i, sec := range f.Sections {
			if sec.Flags&0x08000000 != 0 { // S_ATTR_LIVE_SUPPORT
				for _, a := range bySect[i] {
					g.support[a.name] = g.refs[a.name]
				}
			}
		}
	}()
	// Where every symbol is: a relocation against a section start label
	// (ltmpN, how LLVM names private data on arm64) plus its addend is the
	// atom at that address.
	symAt := map[int][2]uint64{} // symbol index -> section, address
	for k, y := range f.Symtab.Syms {
		if y.Sect != 0 && y.Type&0x0e == 0x0e {
			symAt[k] = [2]uint64{uint64(y.Sect) - 1, y.Value}
		}
	}
	data := map[int][]byte{}
	secData := func(i int) []byte {
		if d, ok := data[i]; ok {
			return d
		}
		d, _ := f.Sections[i].Data()
		data[i] = d
		return d
	}
	for i, sec := range f.Sections {
		retained := sec.Flags&0x10000000 != 0 || sec.Name == "__mod_init_func" // S_ATTR_NO_DEAD_STRIP
		if retained {
			for _, a := range bySect[i] {
				g.roots = append(g.roots, a.name)
			}
		}
		var addend int64
		for _, r := range sec.Relocs {
			if r.Type == 10 { // ARM64_RELOC_ADDEND: the next relocation's addend
				addend = int64(int32(r.Value<<8) >> 8)
				continue
			}
			add := addend
			addend = 0
			from := owner(i, sec.Addr+uint64(r.Addr))
			if from == "" {
				continue
			}
			if r.Extern {
				if int(r.Value) >= len(f.Symtab.Syms) {
					continue
				}
				y := f.Symtab.Syms[r.Value]
				name := symName(y)
				if isLtmp(name) {
					if r.Type == 0 && r.Len == 3 { // a pointer: the addend is in the data
						if d := secData(i); int(r.Addr)+8 <= len(d) {
							add = int64(binary.LittleEndian.Uint64(d[r.Addr:]))
						}
					}
					if at, ok := symAt[int(r.Value)]; ok {
						if t := owner(int(at[0]), at[1]+uint64(add)); t != "" {
							name = t
						}
					}
				}
				g.edge(from, name)
				continue
			}
			// A reference to a section by number: every symbol of it
			// (conservative; LLVM refers to symbols by name on arm64).
			if t := int(r.Value) - 1; t >= 0 {
				for _, a := range bySect[t] {
					g.edge(from, a.name)
				}
			}
		}
	}
	return nil
}

// isLtmp: a Mach-O section start label (local, so named obj#ltmpN).
func isLtmp(n string) bool {
	_, rest, ok := strings.Cut(n, "#")
	return ok && strings.HasPrefix(rest, "ltmp")
}

// live marks every symbol reachable from the roots and from extra roots.
func (g *refGraph) live(extra ...string) map[string]bool {
	seen := map[string]bool{}
	work := append(append([]string{}, g.roots...), extra...)
	mark := func() {
		for len(work) > 0 {
			s := work[len(work)-1]
			work = work[:len(work)-1]
			if seen[s] {
				continue
			}
			seen[s] = true
			work = append(work, g.refs[s]...)
		}
	}
	mark()
	// A support node is live once something it points at is: rounds until
	// none changes (each round marks what the new ones reach).
	for changed := true; changed; {
		changed = false
		for n, targets := range g.support {
			if seen[n] {
				continue
			}
			for _, t := range targets {
				if seen[t] {
					work = append(work, n)
					changed = true
					break
				}
			}
		}
		mark()
	}
	return seen
}
