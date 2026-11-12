package main

// gcdata.go: an independent check of the pointer bitmaps the compiler emits in type
// descriptors (docs/verify.md, "Precise GC contract"). It reads the descriptors from the
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
)

// readDescs collects type descriptors and their bitmaps from an MLIR file.
func readDescs(path string) (map[string]*tdesc, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	descs := map[string]*tdesc{}
	bitsOf := map[string][]byte{}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	var cur string
	var isBits bool
	var i64s []int64
	var i8s []byte
	var gcref bool
	for sc.Scan() {
		line := sc.Text()
		if m := reDescStart.FindStringSubmatch(line); m != nil {
			cur = m[1]
			if i := strings.LastIndex(cur, "."); i >= 0 && strings.Contains("fields methods imethods uncommon", cur[i+1:]) && !strings.HasSuffix(cur, ".gcdata") {
				cur = "" // side tables of a descriptor, not descriptors
				continue
			}
			isBits = strings.HasSuffix(cur, ".gcdata")
			if isBits {
				cur = strings.TrimSuffix(cur, ".gcdata")
			}
			i64s, i8s, gcref = nil, nil, false
			continue
		}
		if cur == "" {
			continue
		}
		if strings.Contains(line, "llvm.return") {
			if isBits {
				bitsOf[cur] = i8s
			} else if len(i64s) >= 2 {
				descs[cur] = &tdesc{name: cur, size: i64s[0], ptrdata: i64s[1], hasBits: gcref}
			}
			cur = ""
			continue
		}
		if isBits {
			if m := reConstI8.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[1])
				i8s = append(i8s, byte(n))
			}
			continue
		}
		if len(i64s) < 2 {
			if m := reConstI64.FindStringSubmatch(line); m != nil {
				n, _ := strconv.ParseInt(m[1], 10, 64)
				i64s = append(i64s, n)
				continue
			}
		}
		if len(i64s) == 2 && strings.Contains(line, `@"type:`+cur+`.gcdata"`) {
			gcref = true
		}
	}
	for k, b := range bitsOf {
		if d := descs[k]; d != nil {
			d.bits = b
		}
	}
	return descs, sc.Err()
}

// ---- gc layout, written from the gc rules (cmd/compile/internal/types/size.go) ----

type lay struct {
	size, align int64
	ptr         []bool // per 8-byte word
}

func layoutOf(t types.Type, depth int) (lay, error) {
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
		case types.Int, types.Uint, types.Int64, types.Uint64, types.Uintptr, types.Float64:
			return lay{8, 8, []bool{false}}, nil
		case types.Complex64:
			return lay{8, 4, []bool{false}}, nil
		case types.Complex128:
			return lay{16, 8, []bool{false, false}}, nil
		case types.String:
			return lay{16, 8, []bool{true, false}}, nil
		case types.UnsafePointer:
			return lay{8, 8, []bool{true}}, nil
		}
		return lay{}, fmt.Errorf("basic %v", u)
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature:
		return lay{8, 8, []bool{true}}, nil
	case *types.Slice:
		return lay{24, 8, []bool{true, false, false}}, nil
	case *types.Interface:
		return lay{16, 8, []bool{true, true}}, nil
	case *types.Array:
		e, err := layoutOf(u.Elem(), depth+1)
		if err != nil {
			return lay{}, err
		}
		n := u.Len()
		out := lay{size: e.size * n, align: e.align}
		if e.size*n > 1<<24 {
			return lay{}, fmt.Errorf("array too large to lay out")
		}
		out.ptr = make([]bool, (out.size+7)/8)
		for i := int64(0); i < n; i++ {
			mark(out.ptr, e, i*e.size)
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
			l, err := layoutOf(u.Field(i).Type(), depth+1)
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
		out := lay{size: size, align: align, ptr: make([]bool, (size+7)/8)}
		for _, f := range fs {
			mark(out.ptr, f.l, f.off)
		}
		return out, nil
	}
	return lay{}, fmt.Errorf("unsupported type %v", t)
}

// mark copies e's pointer words into dst at byte offset off (pointers are 8-aligned).
func mark(dst []bool, e lay, off int64) {
	for w, p := range e.ptr {
		if p {
			dst[off/8+int64(w)] = true
		}
	}
}

// ---- type strings back to types ----

type resolver struct {
	named map[string]*types.TypeName // "pkgpath.Name"
	paths []string                   // package paths, longest first
}

func newResolver(pkgs []*packages.Package) *resolver {
	r := &resolver{named: map[string]*types.TypeName{}}
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

func isIdentByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (r *resolver) conv(e ast.Expr, ph map[string]string) (types.Type, error) {
	switch x := e.(type) {
	case *ast.Ident:
		if q, ok := ph[x.Name]; ok {
			tn := r.named[q]
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
// types), its build tags, and GOARCH arm64 for the layout. The merge itself is file
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
	return append(os.Environ(), "GOROOT="+gr, "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=arm64",
		"GOFLAGS=-tags=marmot,purego,math_big_pure_go", "GOTOOLCHAIN=local"), nil
}

// ---- the check ----

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
	gcSizes := types.SizesFor("gc", "arm64")
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
		l, err := layoutOf(t, 0)
		if err != nil {
			res.skipped++
			res.skips = append(res.skips, n+": "+err.Error())
			continue
		}
		res.checked++
		var problems []string
		if l.size != d.size {
			problems = append(problems, fmt.Sprintf("size %d, gc layout %d", d.size, l.size))
		}
		if gs := gcSizes.Sizeof(t); gs != l.size {
			problems = append(problems, fmt.Sprintf("verify's layout %d disagrees with go/types gc sizes %d", l.size, gs))
		}
		last := int64(-1)
		for w, p := range l.ptr {
			if p {
				last = int64(w)
			}
		}
		if want := (last + 1) * 8; want != d.ptrdata {
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
		if len(problems) > 0 {
			res.bad = append(res.bad, n+": "+strings.Join(problems, "; "))
		}
	}
	return res, nil
}
