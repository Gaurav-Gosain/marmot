package main

// gcsites.go: allocation sites against their descriptors (docs/verify.md, "Precise GC
// contract"). Every go.new, go.make_slice, go.box and go.make_closure names the type
// descriptor the runtime will scan the object with and the LLVM memory type the code stores
// into it. The object kind follows from the descriptor (ptrdata 0: NOSCAN; no bitmap:
// CONSERVATIVE; else PRECISE with the bitmap). Every pointer-typed word of the memory type
// must be a pointer word of the descriptor (or the descriptor must be conservative), and the
// descriptor must be at least as large as the memory type. A NOSCAN descriptor on a memory
// type that holds a pointer is the failure this hunts.

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type site struct {
	op, desc, mtype, line string
}

var (
	reNew     = regexp.MustCompile(`go\.new @"type:(.*?)", (.*?)(?: loc\(|$)`)
	reMkSlice = regexp.MustCompile(`go\.make_slice @"type:(.*?)", (.*?)\(%`)
	reBox     = regexp.MustCompile(`go\.box @"type:(.*?)", %[\w.$-]+ : (.*?)(?: loc\(|$)`)
	reClosure = regexp.MustCompile(`go\.make_closure @.*?, @"type:(.*?)" : \((.*?)\) -> !llvm\.ptr`)
	reMapUpd  = regexp.MustCompile(`go\.map_update %[\w.$-]+\[%[\w.$-]+\] = %[\w.$-]+, @"type:(map\[.*?)" : (.*?)(?: loc\(|$)`)
	reMapLook = regexp.MustCompile(`go\.map_lookup %[\w.$-]+\[%[\w.$-]+\], @"type:(map\[.*?)" : (.*?) -> (.*?)(?: loc\(|$)`)
)

// splitMapType splits "map[K]V" into K and V.
func splitMapType(s string) (k, v string, ok bool) {
	if !strings.HasPrefix(s, "map[") {
		return "", "", false
	}
	depth := 0
	for i := 3; i < len(s); i++ {
		switch s[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
			if depth == 0 && s[i] == ']' {
				return s[4:i], s[i+1:], true
			}
		}
	}
	return "", "", false
}

// splitTop splits "A, B" at the top-level comma of two LLVM types.
func splitTop(s string) (a, b string, ok bool) {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<', '(':
			depth++
		case '>', ')':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
			}
		}
	}
	return "", "", false
}

func readSites(path string) ([]site, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var out []site
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.Contains(l, "go.new @"):
			if m := reNew.FindStringSubmatch(l); m != nil {
				out = append(out, site{"go.new", m[1], strings.TrimSpace(m[2]), l})
			}
		case strings.Contains(l, "go.make_slice @"):
			if m := reMkSlice.FindStringSubmatch(l); m != nil {
				out = append(out, site{"go.make_slice", m[1], strings.TrimSpace(m[2]), l})
			}
		case strings.Contains(l, "go.box @"):
			if m := reBox.FindStringSubmatch(l); m != nil {
				out = append(out, site{"go.box", m[1], strings.TrimSpace(m[2]), l})
			}
		case strings.Contains(l, "go.map_update "):
			if m := reMapUpd.FindStringSubmatch(l); m != nil {
				k, v, ok1 := splitMapType(m[1])
				kt, vt, ok2 := splitTop(m[2])
				if ok1 && ok2 {
					out = append(out, site{"map key", k, kt, l}, site{"map element", v, vt, l})
				}
			}
		case strings.Contains(l, "go.map_lookup "):
			if m := reMapLook.FindStringSubmatch(l); m != nil {
				if k, v, ok := splitMapType(m[1]); ok {
					out = append(out, site{"map key", k, strings.TrimSpace(m[2]), l}, site{"map element", v, strings.TrimSpace(m[3]), l})
				}
			}
		case strings.Contains(l, "go.make_closure @"):
			if m := reClosure.FindStringSubmatch(l); m != nil {
				// the object is struct<(ptr, M0, M1, ...)>
				out = append(out, site{"go.make_closure", m[1], "!llvm.struct<(ptr, " + m[2] + ")>", l})
			}
		}
	}
	return out, sc.Err()
}

// ---- LLVM memory type layout (natural alignment, 64-bit) ----

type llay struct {
	size, align int64
	ptrs        []int64 // byte offsets of ptr fields
}

type tparser struct {
	s string
	i int
}

func (p *tparser) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == ',') {
		p.i++
	}
}

func (p *tparser) eat(t string) bool {
	p.skip()
	if strings.HasPrefix(p.s[p.i:], t) {
		p.i += len(t)
		return true
	}
	return false
}

func (p *tparser) typ() (llay, error) {
	p.skip()
	switch {
	case p.eat("!llvm.ptr"), p.eat("ptr"):
		return llay{8, 8, []int64{0}}, nil
	case p.eat("!llvm.struct<"):
		packed := p.eat("packed")
		if !p.eat("(") {
			// named or opaque struct: unsupported
			return llay{}, fmt.Errorf("named struct at %q", p.s[p.i:])
		}
		var off, align int64 = 0, 1
		var ptrs []int64
		for {
			p.skip()
			if p.eat(")") {
				break
			}
			f, err := p.typ()
			if err != nil {
				return llay{}, err
			}
			a := f.align
			if packed {
				a = 1
			}
			off = (off + a - 1) / a * a
			for _, q := range f.ptrs {
				ptrs = append(ptrs, off+q)
			}
			off += f.size
			if a > align {
				align = a
			}
		}
		if !p.eat(">") {
			return llay{}, fmt.Errorf("struct close at %q", p.s[p.i:])
		}
		return llay{(off + align - 1) / align * align, align, ptrs}, nil
	case p.eat("!llvm.array<"):
		p.skip()
		j := p.i
		for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
			j++
		}
		var n int64
		fmt.Sscan(p.s[p.i:j], &n)
		p.i = j
		if !p.eat("x") {
			return llay{}, fmt.Errorf("array at %q", p.s[p.i:])
		}
		e, err := p.typ()
		if err != nil {
			return llay{}, err
		}
		if !p.eat(">") {
			return llay{}, fmt.Errorf("array close at %q", p.s[p.i:])
		}
		var ptrs []int64
		if n*int64(len(e.ptrs)) > 1<<20 {
			return llay{}, fmt.Errorf("array too large to enumerate")
		}
		for k := int64(0); k < n; k++ {
			for _, q := range e.ptrs {
				ptrs = append(ptrs, k*e.size+q)
			}
		}
		return llay{n * e.size, e.align, ptrs}, nil
	case p.eat("f64"):
		return llay{8, 8, nil}, nil
	case p.eat("f32"):
		return llay{4, 4, nil}, nil
	case p.eat("i"):
		j := p.i
		for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
			j++
		}
		var bits int64
		fmt.Sscan(p.s[p.i:j], &bits)
		p.i = j
		switch {
		case bits <= 8:
			return llay{1, 1, nil}, nil
		case bits <= 16:
			return llay{2, 2, nil}, nil
		case bits <= 32:
			return llay{4, 4, nil}, nil
		case bits <= 64:
			return llay{8, 8, nil}, nil
		default:
			return llay{16, 16, nil}, nil
		}
	}
	return llay{}, fmt.Errorf("unknown type at %q", p.s[p.i:])
}

func parseLLVMType(s string) (llay, error) {
	p := &tparser{s: s}
	l, err := p.typ()
	if err != nil {
		return l, err
	}
	p.skip()
	if p.i != len(p.s) {
		return l, fmt.Errorf("trailing %q", p.s[p.i:])
	}
	return l, nil
}

type sitesResult struct {
	checked, skipped int
	bad, skips       []string
}

func checkSites(mlirPath string) (*sitesResult, error) {
	descs, err := readDescs(mlirPath)
	if err != nil {
		return nil, err
	}
	sites, err := readSites(mlirPath)
	if err != nil {
		return nil, err
	}
	res := &sitesResult{}
	seen := map[string]bool{}
	for _, s := range sites {
		key := s.op + "|" + s.desc + "|" + s.mtype
		if seen[key] {
			continue
		}
		seen[key] = true
		d := descs[s.desc]
		l, err := parseLLVMType(s.mtype)
		if d == nil || err != nil {
			res.skipped++
			why := "no descriptor in this module"
			if err != nil {
				why = err.Error()
			}
			res.skips = append(res.skips, fmt.Sprintf("%s %s %s: %s", s.op, s.desc, s.mtype, why))
			continue
		}
		res.checked++
		var problems []string
		elem := s.op == "go.make_slice" || strings.HasPrefix(s.op, "map ")
		if l.size > d.size && !elem {
			problems = append(problems, fmt.Sprintf("memory type %d bytes, descriptor %d", l.size, d.size))
		}
		if elem && l.size != d.size {
			problems = append(problems, fmt.Sprintf("element memory type %d bytes, descriptor %d (stride)", l.size, d.size))
		}
		for _, off := range l.ptrs {
			// word 0 of a closure is the code pointer to static code: never needs scanning
			if s.op == "go.make_closure" && off == 0 {
				continue
			}
			switch {
			case off%8 != 0:
				problems = append(problems, fmt.Sprintf("pointer at unaligned offset %d", off))
			case d.ptrdata == 0:
				problems = append(problems, fmt.Sprintf("NOSCAN descriptor, pointer stored at offset %d", off))
			case !d.hasBits:
				// conservative: every word is scanned
			default:
				w := off / 8
				if w >= int64(len(d.bits))*8 || d.bits[w/8]&(1<<(w%8)) == 0 {
					problems = append(problems, fmt.Sprintf("pointer stored at offset %d, descriptor word %d is not a pointer", off, w))
				}
			}
			if len(problems) > 3 {
				break
			}
		}
		if len(problems) > 0 {
			res.bad = append(res.bad, fmt.Sprintf("%s %q with %s: %s", s.op, s.desc, s.mtype, strings.Join(problems, "; ")))
		}
	}
	return res, nil
}
