// The pc table: function names, files and lines for code addresses, built
// by the backend into the binary (docs/pcln.md, version 2). It serves
// runtime.Caller, runtime.Callers frames, FuncForPC and panic tracebacks.
//
// The table is read in place; lookups allocate nothing except when a name
// or file is returned as one string (prefix and rest are stored apart).
// Pure file.

package runtime

import "unsafe"

const pclnVersion = 2

// pclnTable is a decoded header of the pc table.
type pclnTable struct {
	data    unsafe.Pointer // the table bytes
	size    uintptr
	base    uintptr // runtime address that function starts are relative to
	nfuncs  uint32
	nfiles  uint32
	ndirs   uint32
	npkgs   uint32
	quantum uint32
	textEnd uint32
	funcOff uint32
	fileOff uint32
	dirOff  uint32
	pkgOff  uint32
	lineOff uint32
	strOff  uint32
}

func (t *pclnTable) u32(off uint32) uint32 {
	p := unsafe.Add(t.data, uintptr(off))
	return uint32(*(*uint8)(p)) | uint32(*(*uint8)(unsafe.Add(p, 1)))<<8 |
		uint32(*(*uint8)(unsafe.Add(p, 2)))<<16 | uint32(*(*uint8)(unsafe.Add(p, 3)))<<24
}

func (t *pclnTable) byteAt(off uint32) uint8 {
	return *(*uint8)(unsafe.Add(t.data, uintptr(off)))
}

func (t *pclnTable) uvarint(off uint32) (uint32, uint32) {
	var v uint32
	var s uint
	for {
		c := t.byteAt(off)
		off++
		v |= uint32(c&0x7f) << s
		s += 7
		if c < 0x80 || s >= 35 {
			return v, off
		}
	}
}

// pclnInit checks the header of the table at data and fills t. It reports
// false for a missing or unknown table.
func (t *pclnTable) init(data unsafe.Pointer, size, base uintptr) bool {
	const hdr = 8 + 13*4
	if data == nil || size < hdr {
		return false
	}
	magic := "MRMTPCLN"
	for i := 0; i < len(magic); i++ {
		if *(*uint8)(unsafe.Add(data, i)) != magic[i] {
			return false
		}
	}
	t.data, t.size, t.base = data, size, base
	if t.u32(8) != pclnVersion {
		return false
	}
	t.nfuncs = t.u32(12)
	t.nfiles = t.u32(16)
	t.ndirs = t.u32(20)
	t.npkgs = t.u32(24)
	t.quantum = t.u32(28)
	t.textEnd = t.u32(32)
	t.funcOff = t.u32(36)
	t.fileOff = t.u32(40)
	t.dirOff = t.u32(44)
	t.pkgOff = t.u32(48)
	t.lineOff = t.u32(52)
	t.strOff = t.u32(56)
	return uintptr(t.funcOff)+uintptr(t.nfuncs)*12 <= size && uintptr(t.strOff) <= size
}

// funcStart returns the start offset of function k.
func (t *pclnTable) funcStart(k uint32) uint32 { return t.u32(t.funcOff + 12*k) }

// findFunc returns the index of the function holding offset off.
func (t *pclnTable) findFunc(off uint32) (uint32, bool) {
	if t.nfuncs == 0 || off >= t.textEnd || off < t.funcStart(0) {
		return 0, false
	}
	lo, hi := uint32(0), t.nfuncs // invariant: start(lo) <= off, start(hi) > off
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if t.funcStart(mid) <= off {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo, true
}

// cstr returns the NUL terminated string at strOff+off without copying.
func (t *pclnTable) cstr(off uint32) string {
	p := unsafe.Add(t.data, uintptr(t.strOff)+uintptr(off))
	n := 0
	for *(*uint8)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return unsafe.String((*byte)(p), n)
}

// record splits the prefixed string record at off into its prefix (from
// the table of string offsets at tab) and the rest.
func (t *pclnTable) record(off, tab, ntab uint32) (prefix, rest string) {
	rec := t.cstr(off)
	idx, n := t.uvarint(t.strOff + off)
	rest = rec[n-(t.strOff+off):]
	if idx >= 1 && idx <= ntab {
		prefix = t.cstr(t.u32(tab + 4*(idx-1)))
	}
	return prefix, rest
}

// funcName returns the name of function k as package prefix and rest.
func (t *pclnTable) funcName(k uint32) (string, string) {
	return t.record(t.u32(t.funcOff+12*k+4), t.pkgOff, t.npkgs)
}

// fileName returns file f as directory prefix and rest.
func (t *pclnTable) fileName(f uint32) (string, string) {
	if f >= t.nfiles {
		return "", "?"
	}
	return t.record(t.u32(t.fileOff+4*f), t.dirOff, t.ndirs)
}

// line returns the file index and line of offset off in function k.
func (t *pclnTable) line(k, off uint32) (file, line uint32) {
	i := t.lineOff + t.u32(t.funcOff+12*k+8)
	n, i := t.uvarint(i)
	line, i = t.uvarint(i)
	file, i = t.uvarint(i)
	pc := t.funcStart(k)
	l, f := line, file
	for ; n > 0; n-- {
		var d, z uint32
		d, i = t.uvarint(i)
		z, i = t.uvarint(i)
		v := int32(z>>1) ^ -int32(z&1)
		changed := v & 1
		pc += d * t.quantum
		l = uint32(int32(l) + (v-changed)/2)
		if changed != 0 {
			f, i = t.uvarint(i)
		}
		if pc > off {
			break
		}
		line, file = l, f
	}
	return file, line
}

// pcInfo describes a code address.
type pcInfo struct {
	entry      uintptr // start address of the function
	namePrefix string
	name       string
	filePrefix string
	file       string
	line       int
}

// lookup describes pc, an address inside a function (callers pass a
// return address minus one).
func (t *pclnTable) lookup(pc uintptr) (pcInfo, bool) {
	if t.data == nil || pc < t.base || pc-t.base >= 1<<32 {
		return pcInfo{}, false
	}
	off := uint32(pc - t.base)
	k, ok := t.findFunc(off)
	if !ok {
		return pcInfo{}, false
	}
	var r pcInfo
	r.entry = t.base + uintptr(t.funcStart(k))
	r.namePrefix, r.name = t.funcName(k)
	f, l := t.line(k, off)
	if l != 0 {
		// Line 0: a function without line information (C code of the
		// runtime core), listed by name only.
		r.filePrefix, r.file = t.fileName(f)
	}
	r.line = int(l)
	return r, true
}
