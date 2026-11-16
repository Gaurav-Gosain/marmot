package main

// gcdata.go: an independent check of the pointer bitmaps the compiler emits in type
// descriptors (precise GC contract). It reads the descriptors from the
// MLIR (globals "type:<T>" with size and ptrdata first, and "type:<T>.gcdata" with one bit
// per word), parses each type string back into a go/types type, computes the bitmap with its
// own implementation of gc's layout rules (shares no code with internal/codegen), and
// compares. Types it cannot resolve (packages replaced by marmot's lib/src overlay, types
// local to a function) are counted as skipped, not passed.

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/loader"
	"golang.org/x/tools/go/packages"
)

type tdesc struct {
	name    string
	size    int64
	ptrdata int64
	hasBits bool
	bits    []byte
}

var (
	reDescStart = regexp.MustCompile(`^\s*llvm\.mlir\.global .*constant @"type:(.*)"\(\)`)
	reConstI64  = regexp.MustCompile(`llvm\.mlir\.constant\((-?\d+) : i64\)`)
	reConstI8   = regexp.MustCompile(`llvm\.mlir\.constant\((-?\d+) : i8\)`)
	// reAnyDesc finds descriptor globals in any form, loosely: readDescs fails when one it
	// finds is not decoded, so a change of the format is an error, not an empty check.
	reAnyDesc = regexp.MustCompile(`llvm\.mlir\.global [^@]*@"type:((?:[^"\\]|\\.)*)"`)
	reGoInit  = regexp.MustCompile(`go\.init = "((?:[^"\\]|\\.)*)"`)
)

// readGoInit decodes a go.init initializer (internal/codegen/consts.go, appendInit): i<n>;
// an integer, z a zero value, f<16 hex digits> a float, a<len>:<symbol> an address, ( ... )
// an aggregate. It returns the scalar values in order (z and a float count as 0; an address
// is not a value) and the symbols the addresses name.
func readGoInit(s string) (vals []int64, syms []string, err error) {
	for i := 0; i < len(s); {
		switch s[i] {
		case '(', ')':
			i++
		case 'z':
			vals = append(vals, 0)
			i++
		case 'i':
			j := strings.IndexByte(s[i:], ';')
			if j < 0 {
				return nil, nil, fmt.Errorf("integer without ';' at %d", i)
			}
			v, perr := strconv.ParseInt(s[i+1:i+j], 10, 64)
			if perr != nil {
				return nil, nil, perr
			}
			vals = append(vals, v)
			i += j + 1
		case 'f':
			if i+17 > len(s) {
				return nil, nil, fmt.Errorf("short float at %d", i)
			}
			vals = append(vals, 0)
			i += 17
		case 'a':
			j := strings.IndexByte(s[i:], ':')
			if j < 0 {
				return nil, nil, fmt.Errorf("address without ':' at %d", i)
			}
			n, perr := strconv.Atoi(s[i+1 : i+j])
			if perr != nil || i+j+1+n > len(s) {
				return nil, nil, fmt.Errorf("bad address length at %d", i)
			}
			syms = append(syms, s[i+j+1:i+j+1+n])
			i += j + 1 + n
		default:
			return nil, nil, fmt.Errorf("unknown letter %q at %d", s[i], i)
		}
	}
	return vals, syms, nil
}

// reSideSuffix is the suffix of a descriptor's side globals (type:T.fields, .params,
// .uncommon and the others) and of its bitmap (.gcdata): a lower case word. Which word is
// not listed, so a new side table is not mistaken for a descriptor. The price: a real
// descriptor type:B.x is skipped when type:B also exists, as for a type x in a package whose
// path is itself a type name. std has no such package.
var reSideSuffix = regexp.MustCompile(`^[a-z]+$`)

// rawGlobal is one type: global as read, before it is known to be a descriptor, a bitmap or
// a side table.
type rawGlobal struct {
	decoded bool
	vals    []int64  // go.init: the scalar values; region: the first two i64 constants
	i8s     []byte   // region: the i8 constants (a bitmap's bytes)
	syms    []string // go.init: the symbols it takes the address of
	gcref   bool     // region: the descriptor refers to its bitmap
	goInit  bool
}

// readDescs collects type descriptors and their bitmaps from an MLIR file. A global
// type:T.S is a side table (S fields, methods, imethods, uncommon) or the bitmap (S gcdata)
// of type:T only when type:T exists: a type may itself be named p.fields or p.data.
func readDescs(path string) (map[string]*tdesc, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	raw := map[string]*rawGlobal{}
	seen := map[string]bool{} // every type: global, found loosely
	refs := 0                 // lines outside the globals that name a type: symbol
	decls := 0                // type: globals declared here and defined in another module
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	var cur *rawGlobal
	var curName string
	for sc.Scan() {
		line := sc.Text()
		if m := reAnyDesc.FindStringSubmatch(line); m != nil {
			// a declaration (defined in another module) has no initializer: neither go.init
			// nor a region
			if strings.Contains(line, " external ") && !strings.Contains(line, "go.init") && !strings.HasSuffix(strings.TrimSpace(line), "{") {
				decls++
			} else {
				seen[m[1]] = true
			}
		} else if strings.Contains(line, `@"type:`) {
			refs++
		}
		if m := reDescStart.FindStringSubmatch(line); m != nil {
			curName, cur = m[1], &rawGlobal{}
			raw[curName] = cur
			if g := reGoInit.FindStringSubmatch(line); g != nil {
				// the one-line form: the initializer is the go.init string
				vals, syms, err := readGoInit(unescapeMLIR(g[1]))
				if err != nil {
					return nil, fmt.Errorf("type:%s: go.init: %v", curName, err)
				}
				cur.vals, cur.syms, cur.decoded, cur.goInit = vals, syms, true, true
				cur = nil
			}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.Contains(line, "llvm.return") {
			cur.decoded = true
			cur = nil
			continue
		}
		if m := reConstI8.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			cur.i8s = append(cur.i8s, byte(n))
			continue
		}
		if len(cur.vals) < 2 {
			if m := reConstI64.FindStringSubmatch(line); m != nil {
				n, _ := strconv.ParseInt(m[1], 10, 64)
				cur.vals = append(cur.vals, n)
				continue
			}
		}
		if len(cur.vals) == 2 && strings.Contains(line, `@"type:`+curName+`.gcdata"`) {
			cur.gcref = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	descs := map[string]*tdesc{}
	bitsOf := map[string][]byte{}
	var missed []string
	for n := range seen {
		base, suf := n, ""
		if i := strings.LastIndex(n, "."); i >= 0 {
			base, suf = n[:i], n[i+1:]
		}
		r := raw[n]
		ok := r != nil && r.decoded
		switch {
		case suf != "gcdata" && reSideSuffix.MatchString(suf) && seen[base]:
			continue
		case suf == "gcdata" && seen[base]:
			if !ok {
				break
			}
			if r.goInit {
				bs := make([]byte, len(r.vals))
				for i, v := range r.vals {
					bs[i] = byte(v)
				}
				bitsOf[base] = bs
			} else {
				bitsOf[base] = r.i8s
			}
			continue
		default:
			if ok && len(r.vals) >= 2 {
				d := &tdesc{name: n, size: r.vals[0], ptrdata: r.vals[1], hasBits: r.gcref}
				if r.goInit {
					d.hasBits = slices.Contains(r.syms, "type:"+unescapeMLIR(n)+".gcdata")
				}
				descs[n] = d
				continue
			}
		}
		missed = append(missed, n)
	}
	if refs > 0 && len(seen) == 0 && decls == 0 {
		return nil, fmt.Errorf("%d lines name type: symbols but no type: global was found: did the descriptor format change?", refs)
	}
	if len(missed) > 0 {
		sort.Strings(missed)
		return nil, fmt.Errorf("%d of %d type: globals not decoded (first: type:%s): did the descriptor format change?", len(missed), len(seen), missed[0])
	}
	for k, b := range bitsOf {
		if d := descs[k]; d != nil {
			d.bits = b
		}
	}
	return descs, nil
}

// ---- gc layout, written from the gc rules (cmd/compile/internal/types/size.go) ----

// model is a target's layout: the pointer size (one bitmap bit per pointer slot) and the size
// of int, uint, uintptr and a length. 64-bit targets use 8 for both. marmot's wasm keeps gc's
// 8-byte integers with 4-byte pointers, so a pointer can sit at offset 4 mod 8 (in
// struct{a int32; p *int}, or as the second word of an interface).
type model struct {
	goarch string
	ps, ws int64
}

// tgt is the target the descriptors were built for (-goarch).
var tgt = mustModel(runtime.GOARCH)

// tgtGOOS is the GOOS the packages are loaded for (-goos).
var tgtGOOS = runtime.GOOS

func modelFor(goarch string) (model, error) {
	switch goarch {
	case "wasm":
		return model{goarch, 4, 8}, nil
	case "amd64", "arm64", "riscv64", "loong64", "ppc64", "ppc64le", "s390x", "mips64", "mips64le":
		return model{goarch, 8, 8}, nil
	}
	return model{}, fmt.Errorf("no layout model for GOARCH=%s", goarch)
}

func mustModel(goarch string) model {
	m, err := modelFor(goarch)
	if err != nil {
		return model{goarch, 8, 8}
	}
	return m
}

// slots is the number of pointer slots of an object of size bytes.
func (m model) slots(size int64) int64 { return (size + m.ps - 1) / m.ps }

// header is the size of a string's or slice's data pointer, padded to the alignment of the
// length that follows it.
func (m model) header() int64 { return (m.ps + m.ws - 1) / m.ws * m.ws }

type lay struct {
	size, align int64
	ptr         []bool // per pointer slot (m.ps bytes); nil when the type has no pointers
}

// ptrAt is the layout of a pointer-sized value: one slot, a pointer when p.
func (m model) ptrAt(p bool) lay { return lay{m.ps, m.ps, []bool{p}} }

func (m model) layoutOf(t types.Type, depth int) (lay, error) {
	if depth > 64 {
		return lay{}, fmt.Errorf("type too deep")
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Bool, types.Int8, types.Uint8:
			return lay{1, 1, nil}, nil
		case types.Int16, types.Uint16:
			return lay{2, 2, nil}, nil
		case types.Int32, types.Uint32, types.Float32:
			return lay{4, 4, nil}, nil
		case types.Int64, types.Uint64, types.Float64:
			return lay{8, 8, nil}, nil
		case types.Int, types.Uint, types.Uintptr:
			return lay{m.ws, m.ws, nil}, nil
		case types.Complex64:
			return lay{8, 4, nil}, nil
		case types.Complex128:
			return lay{16, 8, nil}, nil
		case types.String:
			// {data, len}
			return m.withPtr0(m.header()+m.ws, m.ws), nil
		case types.UnsafePointer:
			return m.ptrAt(true), nil
		}
		return lay{}, fmt.Errorf("basic %v", u)
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature:
		return m.ptrAt(true), nil
	case *types.Slice:
		// {data, len, cap}
		return m.withPtr0(m.header()+2*m.ws, m.ws), nil
	case *types.Interface:
		// {type or itab, data}: two pointer slots
		return lay{2 * m.ps, m.ps, []bool{true, true}}, nil
	case *types.Array:
		e, err := m.layoutOf(u.Elem(), depth+1)
		if err != nil {
			return lay{}, err
		}
		n := u.Len()
		out := lay{size: e.size * n, align: e.align}
		if e.size*n > 1<<24 {
			return lay{}, fmt.Errorf("array too large to lay out")
		}
		if e.ptr != nil {
			out.ptr = make([]bool, m.slots(out.size))
			for i := int64(0); i < n; i++ {
				m.mark(out.ptr, e, i*e.size)
			}
		}
		return out, nil
	case *types.Struct:
		var off, align int64 = 0, 1
		type fl struct {
			l   lay
			off int64
		}
		var fs []fl
		for i := 0; i < u.NumFields(); i++ {
			l, err := m.layoutOf(u.Field(i).Type(), depth+1)
			if err != nil {
				return lay{}, err
			}
			off = (off + l.align - 1) / l.align * l.align
			fs = append(fs, fl{l, off})
			off += l.size
			if l.align > align {
				align = l.align
			}
		}
		// gc: a struct whose last field has size zero gets one byte of padding, so that
		// taking the address of that field does not point past the end of the object.
		if n := len(fs); n > 0 && fs[n-1].l.size == 0 && off > 0 {
			off++
		}
		size := (off + align - 1) / align * align
		out := lay{size: size, align: align}
		for _, f := range fs {
			if f.l.ptr != nil {
				if out.ptr == nil {
					out.ptr = make([]bool, m.slots(size))
				}
				m.mark(out.ptr, f.l, f.off)
			}
		}
		return out, nil
	}
	return lay{}, fmt.Errorf("unsupported type %v", t)
}

// withPtr0 is the layout of a header whose first slot is the only pointer.
func (m model) withPtr0(size, align int64) lay {
	ptr := make([]bool, m.slots(size))
	ptr[0] = true
	return lay{size, align, ptr}
}

// mark copies e's pointer slots into dst at byte offset off (pointers are m.ps-aligned).
func (m model) mark(dst []bool, e lay, off int64) {
	for w, p := range e.ptr {
		if p {
			dst[off/m.ps+int64(w)] = true
		}
	}
}

// ---- type strings back to types ----

type resolver struct {
	named map[string]*types.TypeName   // "pkgpath.Name"
	paths []string                     // package paths, longest first
	local map[string][]*types.TypeName // pkg.Name of types declared inside functions
}

func newResolver(pkgs []*packages.Package) *resolver {
	r := &resolver{named: map[string]*types.TypeName{}, local: map[string][]*types.TypeName{}}
	seen := map[string]bool{}
	var add func(p *packages.Package)
	add = func(p *packages.Package) {
		if seen[p.PkgPath] || p.Types == nil {
			return
		}
		seen[p.PkgPath] = true
		r.paths = append(r.paths, p.PkgPath)
		sc := p.Types.Scope()
		for _, n := range sc.Names() {
			if tn, ok := sc.Lookup(n).(*types.TypeName); ok {
				r.named[p.PkgPath+"."+n] = tn
			}
		}
		var walk func(sc *types.Scope, top bool)
		walk = func(sc *types.Scope, top bool) {
			if !top {
				for _, n := range sc.Names() {
					if tn, ok := sc.Lookup(n).(*types.TypeName); ok {
						r.local[p.PkgPath+"."+n] = append(r.local[p.PkgPath+"."+n], tn)
					}
				}
			}
			for i := 0; i < sc.NumChildren(); i++ {
				walk(sc.Child(i), false)
			}
		}
		walk(sc, true)
		for _, imp := range p.Imports {
			add(imp)
		}
	}
	for _, p := range pkgs {
		add(p)
	}
	sort.Slice(r.paths, func(i, j int) bool { return len(r.paths[i]) > len(r.paths[j]) })
	return r
}

// parse turns a descriptor type string into a go/types type. Qualified names are replaced
// by placeholders first, since package paths are not Go syntax.
func (r *resolver) parse(s string) (types.Type, error) {
	// x/tools/go/ssa's synthetic defer stack type: marmot passes the frame pointer for it
	// (defer.go: frame = f.value(v.DeferStack)), one pointer word, never a Go object
	s = strings.ReplaceAll(s, "$ssa.deferStack", "unsafe.Pointer")
	// MLIR string escapes: \XX is the byte 0xXX (the middle dot of local type names Name·N.M,
	// quotes and backslashes of struct tags)
	s = unescapeMLIR(s)
	// the loader's //go:embed helper type (internal/loader/embed.go embedHelperSrc), which
	// exists only in an overlay file: its declared fields
	s = reEmbedFile.ReplaceAllString(s, "struct{name string; data string; hash [16]byte}")
	ph := map[string]string{}
	var b strings.Builder
	for i := 0; i < len(s); {
		matched := false
		if i == 0 || !isIdentByte(s[i-1]) {
			for _, p := range r.paths {
				if strings.HasPrefix(s[i:], p+".") {
					j := i + len(p) + 1
					k := j
					for k < len(s) && isIdentByte(s[k]) {
						k++
					}
					if strings.HasPrefix(s[k:], "·") {
						k += len("·")
						for k < len(s) && (s[k] == '.' || s[k] >= '0' && s[k] <= '9') {
							k++
						}
					}
					key := s[i:k]
					id := fmt.Sprintf("Q%d_", len(ph))
					ph[id] = key
					b.WriteString(id)
					i = k
					matched = true
					break
				}
			}
		}
		if !matched {
			b.WriteByte(s[i])
			i++
		}
	}
	e, err := parser.ParseExpr(b.String())
	if err != nil {
		return nil, err
	}
	return r.conv(e, ph)
}

var reEmbedFile = regexp.MustCompile(`[\w./-]+\.zz_marmotEmbedFile\b`)

func unescapeMLIR(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b = append(b, byte(v))
				i += 2
				continue
			}
		}
		b = append(b, s[i])
	}
	return string(b)
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (r *resolver) conv(e ast.Expr, ph map[string]string) (types.Type, error) {
	switch x := e.(type) {
	case *ast.Ident:
		if q, ok := ph[x.Name]; ok {
			tn := r.named[q]
			if base, _, ok := strings.Cut(q, "·"); ok {
				switch ls := r.local[base]; len(ls) {
				case 1:
					tn = ls[0]
				case 0:
				default:
					return nil, fmt.Errorf("local type %s: %d declarations of that name", q, len(ls))
				}
			}
			if tn == nil {
				return nil, fmt.Errorf("unknown type %s", q)
			}
			return tn.Type(), nil
		}
		if o, ok := types.Universe.Lookup(x.Name).(*types.TypeName); ok {
			return o.Type(), nil
		}
		return nil, fmt.Errorf("unknown identifier %s", x.Name)
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Name == "unsafe" && x.Sel.Name == "Pointer" {
			return types.Typ[types.UnsafePointer], nil
		}
		return nil, fmt.Errorf("selector %v", x)
	case *ast.StarExpr:
		t, err := r.conv(x.X, ph)
		if err != nil {
			return nil, err
		}
		return types.NewPointer(t), nil
	case *ast.ParenExpr:
		return r.conv(x.X, ph)
	case *ast.ArrayType:
		t, err := r.conv(x.Elt, ph)
		if err != nil {
			return nil, err
		}
		if x.Len == nil {
			return types.NewSlice(t), nil
		}
		lit, ok := x.Len.(*ast.BasicLit)
		if !ok {
			return nil, fmt.Errorf("array length %v", x.Len)
		}
		n, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil {
			return nil, err
		}
		return types.NewArray(t, n), nil
	case *ast.MapType:
		k, err := r.conv(x.Key, ph)
		if err != nil {
			return nil, err
		}
		v, err := r.conv(x.Value, ph)
		if err != nil {
			return nil, err
		}
		return types.NewMap(k, v), nil
	case *ast.ChanType:
		t, err := r.conv(x.Value, ph)
		if err != nil {
			return nil, err
		}
		return types.NewChan(types.SendRecv, t), nil
	case *ast.FuncType:
		return types.NewSignatureType(nil, nil, nil, nil, nil, false), nil // one code pointer word
	case *ast.InterfaceType:
		return types.NewInterfaceType(nil, nil), nil // layout only: two words
	case *ast.StructType:
		var fs []*types.Var
		for _, f := range x.Fields.List {
			t, err := r.conv(f.Type, ph)
			if err != nil {
				return nil, err
			}
			if len(f.Names) == 0 {
				fs = append(fs, types.NewField(token.NoPos, nil, "", t, true))
			}
			for _, n := range f.Names {
				fs = append(fs, types.NewField(token.NoPos, nil, n.Name, t, false))
			}
		}
		return types.NewStruct(fs, nil), nil
	case *ast.IndexExpr, *ast.IndexListExpr:
		var base ast.Expr
		var idx []ast.Expr
		if ie, ok := x.(*ast.IndexExpr); ok {
			base, idx = ie.X, []ast.Expr{ie.Index}
		} else {
			il := x.(*ast.IndexListExpr)
			base, idx = il.X, il.Indices
		}
		g, err := r.conv(base, ph)
		if err != nil {
			return nil, err
		}
		var args []types.Type
		for _, a := range idx {
			t, err := r.conv(a, ph)
			if err != nil {
				return nil, err
			}
			args = append(args, t)
		}
		return types.Instantiate(nil, g, args, false)
	}
	return nil, fmt.Errorf("unsupported type syntax %T", e)
}

// marmotEnv is the go list environment marmot builds with: its merged GOROOT (stock sources
// with lib/src overlaid, so reflect, runtime and the other overlay packages have marmot's
// types), its build tags, and the target's GOOS and GOARCH (-goos, -goarch: the host's by default). The merge itself is file
// placement only (internal/loader/goroot.go); the layout is computed here.
func marmotEnv() ([]string, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "lib", "src", "runtime")); err == nil {
			break
		}
		up := filepath.Dir(root)
		if up == root {
			return nil, fmt.Errorf("no marmot checkout above the working directory")
		}
		root = up
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return nil, err
	}
	gr, err := loader.MergedGOROOT(strings.TrimSpace(string(out)), filepath.Join(root, "lib", "src"), filepath.Join(root, "build"))
	if err != nil {
		return nil, err
	}
	return append(os.Environ(), "GOROOT="+gr, "CGO_ENABLED=0", "GOOS="+tgtGOOS, "GOARCH="+tgt.goarch,
		"GOFLAGS=-tags=marmot,purego,math_big_pure_go", "GOTOOLCHAIN=local"), nil
}

// ---- the check ----

// compare checks a descriptor against the layout: size, ptrdata (the end of the last pointer
// slot) and one bitmap bit per pointer slot.
func (m model) compare(d *tdesc, l lay) []string {
	var problems []string
	if l.size != d.size {
		problems = append(problems, fmt.Sprintf("size %d, gc layout %d", d.size, l.size))
	}
	last := int64(-1)
	for w, p := range l.ptr {
		if p {
			last = int64(w)
		}
	}
	if want := (last + 1) * m.ps; want != d.ptrdata {
		problems = append(problems, fmt.Sprintf("ptrdata %d, want %d", d.ptrdata, want))
	}
	switch {
	case last < 0:
		if d.hasBits {
			problems = append(problems, "bitmap for a type without pointers")
		}
	case !d.hasBits:
		// allowed only when the type is scanned conservatively for size
		if int64(len(l.ptr)) <= 4096 {
			problems = append(problems, "no bitmap: scanned conservatively although small")
		}
	default:
		for w, p := range l.ptr {
			got := w/8 < len(d.bits) && d.bits[w/8]&(1<<(w%8)) != 0
			if got != p {
				problems = append(problems, fmt.Sprintf("word %d: emitted pointer=%v, gc layout pointer=%v", w, got, p))
				break
			}
		}
		for w := len(l.ptr); w < len(d.bits)*8; w++ {
			if d.bits[w/8]&(1<<(w%8)) != 0 {
				problems = append(problems, fmt.Sprintf("word %d: bit set beyond the object", w))
				break
			}
		}
	}
	return problems
}

type gcResult struct {
	checked, skipped int
	bad              []string
	skips            []string
}

func checkGCData(mlirPath string, patterns []string) (*gcResult, error) {
	descs, err := readDescs(mlirPath)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{Mode: packages.NeedTypes | packages.NeedImports | packages.NeedDeps | packages.NeedName}
	env, err := marmotEnv()
	if err != nil {
		return nil, err
	}
	cfg.Env = env
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	// the runtime is an implicit import of every marmot program
	rt, err := packages.Load(cfg, "runtime")
	if err != nil {
		return nil, err
	}
	pkgs = append(pkgs, rt...)
	// the main package of a file-argument build is "command-line-arguments" in both tools
	r := newResolver(pkgs)
	gcSizes := types.SizesFor("gc", tgt.goarch)
	res := &gcResult{}
	names := make([]string, 0, len(descs))
	for n := range descs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		d := descs[n]
		t, err := r.parse(n)
		if err != nil {
			res.skipped++
			res.skips = append(res.skips, n+": "+err.Error())
			continue
		}
		l, err := tgt.layoutOf(t, 0)
		if err != nil {
			res.skipped++
			res.skips = append(res.skips, n+": "+err.Error())
			continue
		}
		res.checked++
		problems := tgt.compare(d, l)
		// gc's own sizes cross-check the model on 64-bit targets. gc's wasm is 64-bit
		// throughout, so it says nothing about marmot's 4-byte wasm pointers.
		if gcSizes != nil && tgt.ps == 8 {
			if gs := gcSizes.Sizeof(t); gs != l.size {
				problems = append(problems, fmt.Sprintf("verify's layout %d disagrees with go/types gc sizes %d", l.size, gs))
			}
		}
		if len(problems) > 0 {
			res.bad = append(res.bad, n+": "+strings.Join(problems, "; "))
		}
	}
	if len(descs) > 0 && res.checked == 0 {
		return nil, fmt.Errorf("none of the %d type descriptors checked (%d skipped)", len(descs), res.skipped)
	}
	return res, nil
}
