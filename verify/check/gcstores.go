package main

// gcstores.go: pointer stores into allocated objects against the kind the object was allocated
// with (docs/verify.md, "Precise GC contract"). The declared memory type of an allocation site
// (gcsites.go) says what the compiler meant to store; this file checks what it does store. In
// every function it follows the pointer returned by each allocation (go.new, go.make_slice,
// runtime.newobject, runtime.makeslice, runtime.growslice, rt_region_alloc on a noscan region)
// through extractvalue, getelementptr and block arguments, and checks each llvm.store through a
// derived pointer: every pointer word of the stored type must land on a pointer word of the
// object's descriptor. A pointer stored into a NOSCAN object (descriptor ptrdata 0, or a noscan
// region) is invisible to the collector. Pointers that leave the function, go through memory or
// through calls are not followed; the counts say how many stores were checked.

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ttype is an LLVM memory type with field offsets, for getelementptr.
type ttype struct {
	size, align int64
	ptrs        []int64
	fields      []*ttype // struct
	offs        []int64  // struct field offsets
	elem        *ttype   // array
	n           int64
}

type ttparser struct {
	s string
	i int
}

func (p *ttparser) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == ',') {
		p.i++
	}
}

func (p *ttparser) eat(t string) bool {
	p.skip()
	if strings.HasPrefix(p.s[p.i:], t) {
		p.i += len(t)
		return true
	}
	return false
}

func (p *ttparser) num() int64 {
	p.skip()
	j := p.i
	for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
		j++
	}
	n, _ := strconv.ParseInt(p.s[p.i:j], 10, 64)
	p.i = j
	return n
}

func (p *ttparser) typ() (*ttype, error) {
	p.skip()
	switch {
	case p.eat("!llvm.ptr"), p.eat("ptr"):
		return &ttype{size: 8, align: 8, ptrs: []int64{0}}, nil
	case p.eat("!llvm.struct<"):
		packed := p.eat("packed")
		if !p.eat("(") {
			return nil, fmt.Errorf("named struct at %q", p.s[p.i:])
		}
		t := &ttype{align: 1}
		var off int64
		for {
			p.skip()
			if p.eat(")") {
				break
			}
			f, err := p.typ()
			if err != nil {
				return nil, err
			}
			a := f.align
			if packed {
				a = 1
			}
			off = (off + a - 1) / a * a
			t.fields = append(t.fields, f)
			t.offs = append(t.offs, off)
			for _, q := range f.ptrs {
				t.ptrs = append(t.ptrs, off+q)
			}
			off += f.size
			if a > t.align {
				t.align = a
			}
		}
		if !p.eat(">") {
			return nil, fmt.Errorf("struct close at %q", p.s[p.i:])
		}
		t.size = (off + t.align - 1) / t.align * t.align
		return t, nil
	case p.eat("!llvm.array<"):
		n := p.num()
		if !p.eat("x") {
			return nil, fmt.Errorf("array at %q", p.s[p.i:])
		}
		e, err := p.typ()
		if err != nil {
			return nil, err
		}
		if !p.eat(">") {
			return nil, fmt.Errorf("array close at %q", p.s[p.i:])
		}
		t := &ttype{size: n * e.size, align: e.align, elem: e, n: n}
		if n*int64(len(e.ptrs)) > 1<<16 {
			return nil, fmt.Errorf("array too large to enumerate")
		}
		for k := int64(0); k < n; k++ {
			for _, q := range e.ptrs {
				t.ptrs = append(t.ptrs, k*e.size+q)
			}
		}
		return t, nil
	case p.eat("f64"):
		return &ttype{size: 8, align: 8}, nil
	case p.eat("f32"):
		return &ttype{size: 4, align: 4}, nil
	case p.eat("f16"), p.eat("bf16"):
		return &ttype{size: 2, align: 2}, nil
	case p.eat("i"):
		bits := p.num()
		switch {
		case bits <= 8:
			return &ttype{size: 1, align: 1}, nil
		case bits <= 16:
			return &ttype{size: 2, align: 2}, nil
		case bits <= 32:
			return &ttype{size: 4, align: 4}, nil
		case bits <= 64:
			return &ttype{size: 8, align: 8}, nil
		default:
			return &ttype{size: 16, align: 16}, nil
		}
	}
	return nil, fmt.Errorf("unknown type at %q", p.s[p.i:])
}

var ttCache = map[string]*ttype{}

func parseTT(s string) (*ttype, error) {
	s = strings.TrimSpace(s)
	if t, ok := ttCache[s]; ok {
		if t == nil {
			return nil, fmt.Errorf("unparsed type %q", s)
		}
		return t, nil
	}
	p := &ttparser{s: s}
	t, err := p.typ()
	if err == nil {
		p.skip()
		if p.i != len(p.s) {
			err = fmt.Errorf("trailing %q", p.s[p.i:])
		}
	}
	if err != nil {
		ttCache[s] = nil
		return nil, err
	}
	ttCache[s] = t
	return t, nil
}

// ---- facts ----

// obj is an allocated object: a descriptor (single object, or an array of elements of that
// descriptor), or a noscan region object.
type obj struct {
	desc   string // type descriptor name; "" for a region object
	array  bool
	region bool // allocated from a noscan region
	site   string
}

// fact: value = pointer into object o at offset off + k*stride (k >= 0), or (agg) a slice
// aggregate whose field 0 is such a pointer, or (rgn) a noscan region handle.
type fact struct {
	o                int
	off, stride, cnt int64 // cnt: number of k values when known, 0 when unbounded
	agg, rgn         bool
}

func gcd(a, b int64) int64 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

var (
	reFuncStart = regexp.MustCompile(`^\s*llvm\.func\b`)
	reBlock     = regexp.MustCompile(`^\s*\^([\w.$-]+)(?:\((.*)\))?:`)
	reDef       = regexp.MustCompile(`^\s*(%[\w.$-]+) = (.*)$`)
	reAddrOf    = regexp.MustCompile(`^llvm\.mlir\.addressof @"type:(.*?)" : !llvm\.ptr`)
	reStConst     = regexp.MustCompile(`^llvm\.mlir\.constant\((-?\d+) : i\d+\)`)
	reRtAlloc   = regexp.MustCompile(`^llvm\.call @runtime\.(newobject|makeslice|makeslice64|growslice)\((%[\w.$-]+)`)
	reRgnNew    = regexp.MustCompile(`^llvm\.call @rt_region_new_noscan\(`)
	reRgnAlloc  = regexp.MustCompile(`^llvm\.call @rt_region_alloc\((%[\w.$-]+),`)
	reGoNew     = regexp.MustCompile(`^go\.new @"type:(.*?)", `)
	reGoMkSl    = regexp.MustCompile(`^go\.make_slice @"type:(.*?)", `)
	reExtract   = regexp.MustCompile(`^llvm\.extractvalue (%[\w.$-]+)\[0\] `)
	reGEP       = regexp.MustCompile(`^llvm\.getelementptr (?:inbounds )?(%[\w.$-]+)\[(.*?)\] : \(.*?\) -> !llvm\.ptr, (.*?)(?: loc\(|$)`)
	reSelect    = regexp.MustCompile(`^llvm\.select %[\w.$-]+, (%[\w.$-]+), (%[\w.$-]+) `)
	reStore     = regexp.MustCompile(`^\s*llvm\.store (?:volatile )?(%[\w.$-]+), (%[\w.$-]+)(?: atomic \w+)?(?: \{.*?\})? : (.*?), !llvm\.ptr`)
	reStSucc      = regexp.MustCompile(`\^([\w.$-]+)\(([^()]*?) : `)
	reBlockArg  = regexp.MustCompile(`(%[\w.$-]+):`)
)

type storesResult struct {
	stores, checked, skipped int
	bad                      []string
}

type fnBody struct {
	name  string
	lines []string
}

func readStoreFuncs(path string) ([]fnBody, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var out []fnBody
	var cur *fnBody
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		l := sc.Text()
		if reFuncStart.MatchString(l) && strings.HasSuffix(strings.TrimSpace(l), "{") {
			name := l
			if i := strings.Index(l, "@"); i >= 0 {
				name = l[i:]
				if j := strings.IndexAny(name, "("); j >= 0 {
					name = name[:j]
				}
			}
			out = append(out, fnBody{name: name})
			cur = &out[len(out)-1]
			continue
		}
		if cur != nil {
			if l == "  }" {
				cur = nil
				continue
			}
			cur.lines = append(cur.lines, l)
		}
	}
	return out, sc.Err()
}

func checkStores(mlirPath string) (*storesResult, error) {
	descs, err := readDescs(mlirPath)
	if err != nil {
		return nil, err
	}
	fns, err := readStoreFuncs(mlirPath)
	if err != nil {
		return nil, err
	}
	res := &storesResult{}
	seenBad := map[string]bool{}
	for _, fn := range fns {
		checkFnStores(fn, descs, res, seenBad)
	}
	return res, nil
}

func checkFnStores(fn fnBody, descs map[string]*tdesc, res *storesResult, seenBad map[string]bool) {
	addr := map[string]string{} // %v -> descriptor name
	consts := map[string]int64{}
	params := map[string][]string{} // block -> its argument names
	var objs []obj
	facts := map[string][]fact{}
	add := func(v string, f fact) bool {
		fs := facts[v]
		for _, g := range fs {
			if g == f {
				return false
			}
		}
		if len(fs) >= 16 {
			return false
		}
		facts[v] = append(fs, f)
		return true
	}
	for _, l := range fn.lines {
		if m := reBlock.FindStringSubmatch(l); m != nil {
			var ps []string
			for _, a := range reBlockArg.FindAllStringSubmatch(m[2], -1) {
				ps = append(ps, a[1])
			}
			params[m[1]] = ps
			continue
		}
		if m := reDef.FindStringSubmatch(l); m != nil {
			if a := reAddrOf.FindStringSubmatch(m[2]); a != nil {
				addr[m[1]] = a[1]
			} else if c := reStConst.FindStringSubmatch(m[2]); c != nil {
				n, _ := strconv.ParseInt(c[1], 10, 64)
				consts[m[1]] = n
			}
		}
	}
	// roots, in program order (they do not depend on facts)
	for _, l := range fn.lines {
		m := reDef.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		v, rhs := m[1], m[2]
		switch {
		case reGoNew.MatchString(rhs):
			objs = append(objs, obj{desc: reGoNew.FindStringSubmatch(rhs)[1], site: "go.new"})
			add(v, fact{o: len(objs) - 1})
		case reGoMkSl.MatchString(rhs):
			objs = append(objs, obj{desc: reGoMkSl.FindStringSubmatch(rhs)[1], array: true, site: "go.make_slice"})
			add(v, fact{o: len(objs) - 1, agg: true})
		case reRtAlloc.MatchString(rhs):
			a := reRtAlloc.FindStringSubmatch(rhs)
			d, ok := addr[a[2]]
			if !ok {
				continue
			}
			o := obj{desc: d, site: "runtime." + a[1]}
			if a[1] != "newobject" {
				o.array = true
			}
			objs = append(objs, o)
			add(v, fact{o: len(objs) - 1, agg: a[1] == "growslice"})
		case reRgnNew.MatchString(rhs):
			add(v, fact{o: -1, rgn: true})
		}
	}
	if len(objs) == 0 {
		return
	}
	// propagate to a fixed point
	for changed := true; changed; {
		changed = false
		for _, l := range fn.lines {
			if m := reDef.FindStringSubmatch(l); m != nil {
				v, rhs := m[1], m[2]
				switch {
				case reExtract.MatchString(rhs):
					src := reExtract.FindStringSubmatch(rhs)[1]
					for _, f := range facts[src] {
						if f.agg {
							g := f
							g.agg = false
							changed = add(v, g) || changed
						}
					}
				case reRgnAlloc.MatchString(rhs):
					r := reRgnAlloc.FindStringSubmatch(rhs)[1]
					for _, f := range facts[r] {
						if f.rgn {
							objs = append(objs, obj{region: true, site: "rt_region_alloc(noscan)"})
							changed = add(v, fact{o: len(objs) - 1}) || changed
						}
					}
				case reGEP.MatchString(rhs):
					g := reGEP.FindStringSubmatch(rhs)
					fs := facts[g[1]]
					if len(fs) == 0 {
						continue
					}
					off, stride, cnt, ok := gepOffset(g[2], g[3], consts)
					if !ok {
						continue
					}
					for _, f := range fs {
						if f.agg || f.rgn {
							continue
						}
						n := fact{o: f.o, off: f.off + off, stride: f.stride, cnt: f.cnt}
						if stride != 0 {
							if f.stride != 0 && (f.stride != stride || f.cnt != 0 || cnt != 0) {
								continue // two dynamic indexes: not followed
							}
							n.stride, n.cnt = stride, cnt
						}
						changed = add(v, n) || changed
					}
				case reSelect.MatchString(rhs):
					s := reSelect.FindStringSubmatch(rhs)
					for _, src := range s[1:] {
						for _, f := range facts[src] {
							changed = add(v, f) || changed
						}
					}
				}
			}
			if strings.Contains(l, "llvm.br ") || strings.Contains(l, "llvm.cond_br ") || strings.Contains(l, "llvm.switch ") {
				for _, s := range reStSucc.FindAllStringSubmatch(l, -1) {
					ps := params[s[1]]
					args := strings.Split(s[2], ",")
					for i, a := range args {
						a = strings.TrimSpace(a)
						if i >= len(ps) {
							break
						}
						for _, f := range facts[a] {
							changed = add(ps[i], f) || changed
						}
					}
				}
			}
		}
	}
	for _, l := range fn.lines {
		m := reStore.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		fs := facts[m[2]]
		if len(fs) == 0 {
			continue
		}
		res.stores++
		t, err := parseTT(m[3])
		if err != nil {
			res.skipped++
			continue
		}
		res.checked++
		if len(t.ptrs) == 0 {
			continue
		}
		for _, f := range fs {
			if f.agg || f.rgn {
				continue
			}
			o := objs[f.o]
			if p := storeProblem(o, f, t, descs); p != "" {
				key := fn.name + "|" + o.site + "|" + o.desc + "|" + p
				if !seenBad[key] {
					seenBad[key] = true
					res.bad = append(res.bad, fmt.Sprintf("%s: store of %s into %s %q: %s", fn.name, m[3], o.site, o.desc, p))
				}
			}
		}
	}
}

// gepOffset returns the byte offset a getelementptr adds, as off + k*stride for unknown k when
// one index is dynamic (0 <= k < cnt for an array index, unbounded for the first index). A
// negative constant first index or a second dynamic index is not followed.
func gepOffset(idx, et string, consts map[string]int64) (off, stride, cnt int64, ok bool) {
	t, err := parseTT(et)
	if err != nil {
		return 0, 0, 0, false
	}
	parts := strings.Split(idx, ",")
	val := func(s string) (int64, bool) {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "%") {
			n, ok := consts[s]
			return n, ok
		}
		n, err := strconv.ParseInt(s, 10, 64)
		return n, err == nil
	}
	cur := t
	for i, p := range parts {
		n, known := val(p)
		if i == 0 {
			if known {
				if n < 0 {
					return 0, 0, 0, false
				}
				off += n * cur.size
			} else {
				stride = cur.size
			}
			continue
		}
		switch {
		case cur.fields != nil:
			if !known || n < 0 || int(n) >= len(cur.fields) {
				return 0, 0, 0, false
			}
			off += cur.offs[n]
			cur = cur.fields[n]
		case cur.elem != nil:
			if known {
				off += n * cur.elem.size
			} else {
				if stride != 0 {
					return 0, 0, 0, false
				}
				stride, cnt = cur.elem.size, cur.n
			}
			cur = cur.elem
		default:
			return 0, 0, 0, false
		}
	}
	return off, stride, cnt, true
}

// storeProblem says what is wrong with storing a value of type t at the fact's offsets of o.
func storeProblem(o obj, f fact, t *ttype, descs map[string]*tdesc) string {
	if o.region {
		return fmt.Sprintf("pointer stored at offset %d of a NOSCAN region object", f.off+t.ptrs[0])
	}
	d := descs[o.desc]
	if d == nil {
		return ""
	}
	if d.ptrdata == 0 {
		return fmt.Sprintf("NOSCAN object, pointer stored at offset %d", f.off+t.ptrs[0])
	}
	if !d.hasBits {
		return "" // conservative
	}
	if d.size == 0 {
		return ""
	}
	isPtr := func(pos int64) bool {
		if o.array {
			pos %= d.size
		}
		if pos%8 != 0 {
			return false
		}
		w := pos / 8
		return w < int64(len(d.bits))*8 && d.bits[w/8]&(1<<(w%8)) != 0
	}
	limit := int64(1)
	switch {
	case f.stride > 0 && f.cnt > 0:
		limit = f.cnt
	case f.stride > 0 && o.array:
		limit = d.size / gcd(f.stride, d.size)
	case f.stride > 0:
		limit = d.size/f.stride + 1
	}
	for _, q := range t.ptrs {
		for k := int64(0); k < limit; k++ {
			pos := f.off + q + k*f.stride
			if !o.array && pos >= d.size {
				if k == 0 {
					return fmt.Sprintf("pointer stored at offset %d beyond the %d-byte object", pos, d.size)
				}
				break
			}
			if !isPtr(pos) {
				return fmt.Sprintf("pointer stored at offset %d, descriptor word %d is not a pointer", pos, pos%max(d.size, 1)/8)
			}
		}
	}
	return ""
}
