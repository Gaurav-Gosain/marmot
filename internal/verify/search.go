package verify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/go/ssa"
)

// Hint tells the directed search how to call one function: its
// parameters, the constants it uses (boundary values) and its open
// obligations. The analyzer emits one per function it can search, in a
// diagnostic of category CategorySearch.
type Hint struct {
	PkgName  string `json:"pkg"`
	Fn       string `json:"fn"`   // name in the package
	Full     string `json:"full"` // full SSA name
	Exported bool   `json:"exported"`
	// Recv is the receiver of a method: "T" or "*T" for a named type of
	// the package, called on its zero value; RecvExported says whether
	// any importer can make that value (var v T).
	Recv         string `json:"recv,omitempty"`
	RecvExported bool   `json:"recvExported,omitempty"`
	// Effects is set when the function's calls reach files, processes,
	// the network or system calls: it is run only on request.
	Effects bool `json:"effects,omitempty"`
	// Variadic: the last parameter is ...T, passed with ... in calls.
	Variadic bool     `json:"variadic,omitempty"`
	Params   []Param  `json:"params"`
	Ints     []int64  `json:"ints,omitempty"`
	Strs     []string `json:"strs,omitempty"`
	Obls     []OblRef `json:"obls"`
}

// Param is one parameter: its Go type as written in the package, and its
// kind: int, uint, bool, string, bytes, slice (of Elem kind).
type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Kind string `json:"kind"`
	Bits int    `json:"bits,omitempty"`
	Elem *Param `json:"elem,omitempty"`
}

// OblRef names an open obligation by position.
type OblRef struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
	Status string `json:"status"`
	Class  string `json:"class,omitempty"`
}

// CategorySearch is the category of search hint diagnostics.
const CategorySearch = "verify-search"

// paramOf describes a parameter type the search can build, or nil.
func paramOf(t types.Type, pkg *types.Package, depth int) *Param {
	if depth > 1 {
		return nil
	}
	// io.Reader and friends: a reader over candidate bytes.
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "io" {
		switch n.Obj().Name() {
		case "Reader", "ReaderAt", "ReadSeeker", "ByteReader", "RuneReader":
			return &Param{Type: "marmotio." + n.Obj().Name(), Kind: "reader"}
		}
	}
	// A pointer to a struct or basic named type of the package: a fresh
	// zero value (new(T)), as any importer can make.
	if p, ok := t.(*types.Pointer); ok && depth == 0 {
		if n, ok := types.Unalias(p.Elem()).(*types.Named); ok && n.Obj().Pkg() == pkg && n.TypeParams().Len() == 0 {
			switch n.Underlying().(type) {
			case *types.Struct, *types.Basic, *types.Array:
				return &Param{Type: "*" + n.Obj().Name(), Kind: "zero", Elem: &Param{Type: n.Obj().Name()}}
			}
		}
	}
	if n, ok := types.Unalias(t).(*types.Named); ok {
		if n.Obj().Pkg() != nil && n.Obj().Pkg() != pkg || n.TypeArgs() != nil {
			return nil // a type of another package needs an import
		}
	}
	if _, ok := types.Unalias(t).(*types.TypeParam); ok {
		return nil
	}
	name := types.TypeString(t, func(p *types.Package) string {
		if p == pkg {
			return ""
		}
		return p.Name()
	})
	switch u := t.Underlying().(type) {
	case *types.Basic:
		info := u.Info()
		switch {
		case info&types.IsBoolean != 0:
			return &Param{Type: name, Kind: "bool"}
		case info&types.IsString != 0:
			return &Param{Type: name, Kind: "string"}
		case info&types.IsInteger != 0:
			bits := int(sizes.Sizeof(u)) * 8
			if info&types.IsUnsigned != 0 {
				return &Param{Type: name, Kind: "uint", Bits: bits}
			}
			return &Param{Type: name, Kind: "int", Bits: bits}
		}
	case *types.Slice:
		e := paramOf(u.Elem(), pkg, depth+1)
		if e == nil || e.Kind == "slice" || e.Kind == "bytes" {
			return nil
		}
		if b, ok := u.Elem().(*types.Basic); ok && (b.Kind() == types.Byte || b.Kind() == types.Uint8) {
			return &Param{Type: name, Kind: "bytes"}
		}
		return &Param{Type: name, Kind: "slice", Elem: e}
	}
	return nil
}

// makeHint builds the search hint of a function with open obligations.
func makeHint(fn *ssa.Function, fr *FuncResult, fset *token.FileSet) *Hint {
	pos := func(p token.Pos) (string, int) {
		ps := fset.Position(p)
		return ps.Filename, ps.Line
	}
	col := func(p token.Pos) int { return fset.Position(p).Column }
	if fn.Parent() != nil || fn.TypeParams().Len() > 0 || fn.Pkg == nil || fn.Name() == "init" || fn.Name() == "main" || strings.HasPrefix(fn.Name(), "init#") {
		return nil
	}
	recv, recvExported := "", false
	if r := fn.Signature.Recv(); r != nil {
		t := r.Type()
		ptr := false
		if p, ok := t.(*types.Pointer); ok {
			t, ptr = p.Elem(), true
		}
		n, ok := types.Unalias(t).(*types.Named)
		if !ok || n.Obj().Pkg() != fn.Pkg.Pkg || n.TypeParams().Len() > 0 {
			return nil
		}
		// Zero values only of structs and basic types: a nil map, slice
		// or func receiver is rarely meaningful.
		switch n.Underlying().(type) {
		case *types.Struct, *types.Basic:
		default:
			return nil
		}
		recv, recvExported = n.Obj().Name(), n.Obj().Exported()
		if ptr {
			recv = "*" + recv
		}
	}
	if strings.HasPrefix(fn.Name(), "Test") || strings.HasPrefix(fn.Name(), "Benchmark") || strings.HasPrefix(fn.Name(), "Fuzz") || strings.HasPrefix(fn.Name(), "Example") {
		return nil
	}
	if f := fn.Syntax(); f != nil {
		if file, _ := pos(f.Pos()); strings.HasSuffix(file, "_test.go") {
			return nil
		}
	}
	h := &Hint{PkgName: fn.Pkg.Pkg.Name(), Fn: fn.Name(), Full: fn.String(), Exported: fn.Object() != nil && fn.Object().Exported(), Recv: recv, RecvExported: recvExported, Effects: fr.Effects, Variadic: fn.Signature.Variadic()}
	params := fn.Params
	if recv != "" {
		params = params[1:]
	}
	for _, p := range params {
		pr := paramOf(p.Type(), fn.Pkg.Pkg, 0)
		if pr == nil {
			return nil
		}
		pr.Name = p.Name()
		h.Params = append(h.Params, *pr)
	}
	for _, o := range fr.Obligations {
		if o.Status == Proved || o.Status == ProvedAssuming || o.Status == Recovered || o.Reason == "budget" {
			continue
		}
		file, line := pos(o.Ob.Pos)
		h.Obls = append(h.Obls, OblRef{Key: o.Ob.Key, Kind: string(o.Ob.Kind), File: file, Line: line, Col: col(o.Ob.Pos), Status: string(o.Status), Class: o.Class})
	}
	if len(h.Obls) == 0 {
		return nil
	}
	ints := map[int64]bool{}
	strs := map[string]bool{}
	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			for _, op := range in.Operands(nil) {
				c, ok := (*op).(*ssa.Const)
				if !ok || c.Value == nil {
					continue
				}
				switch c.Value.Kind() {
				case constant.Int:
					if v, ok := constant.Int64Val(c.Value); ok && v > -1<<20 && v < 1<<20 {
						ints[v] = true
					}
				case constant.String:
					if s := constant.StringVal(c.Value); len(s) <= 64 {
						strs[s] = true
					}
				}
			}
		}
	}
	for v := range ints {
		h.Ints = append(h.Ints, v)
	}
	slices.Sort(h.Ints)
	if len(h.Ints) > 40 {
		h.Ints = h.Ints[:40]
	}
	for s := range strs {
		h.Strs = append(h.Strs, s)
	}
	slices.Sort(h.Strs)
	if len(h.Strs) > 20 {
		h.Strs = h.Strs[:20]
	}
	return h
}

// candidates are the values tried for one parameter, as Go expressions
// of its type, small and boundary values first.
func candidates(p Param, h *Hint) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] && len(out) < 160 {
			seen[s] = true
			out = append(out, s)
		}
	}
	switch p.Kind {
	case "bool":
		return []string{"false", "true"}
	case "int", "uint":
		for _, v := range intCandidates(p, h) {
			add(intLit(p, v))
		}
	case "string":
		for _, s := range stringCandidates(h) {
			add(typed(p.Type, strconv.Quote(s)))
		}
	case "zero":
		add("new(" + p.Elem.Type + ")")
	case "reader":
		for _, s := range stringCandidates(h) {
			add("marmotbytes.NewReader([]byte(" + strconv.Quote(s) + "))")
		}
	case "bytes":
		add(p.Type + "(nil)")
		for _, s := range stringCandidates(h) {
			add(p.Type + "(" + strconv.Quote(s) + ")")
		}
	case "slice":
		add(p.Type + "(nil)")
		el := candidates(*p.Elem, h)
		for n := 0; n <= 4; n++ {
			for start := 0; start < len(el) && start < 4; start++ {
				var xs []string
				for i := 0; i < n; i++ {
					xs = append(xs, el[(start+i)%len(el)])
				}
				add(p.Type + "{" + strings.Join(xs, ", ") + "}")
			}
		}
		for _, n := range []int{8, 16} {
			xs := make([]string, n)
			for i := range xs {
				xs[i] = el[0]
			}
			add(p.Type + "{" + strings.Join(xs, ", ") + "}")
		}
	}
	return out
}

func intLit(p Param, v int64) string {
	s := strconv.FormatInt(v, 10)
	if p.Kind == "uint" && v < 0 {
		// The largest value of the type.
		s = strconv.FormatUint(math.MaxUint64>>(64-uint(p.Bits)), 10)
	}
	return typed(p.Type, s)
}

// typed converts a constant literal to a parameter type; predeclared
// types need no conversion (the constant takes the parameter's type).
func typed(t, lit string) string {
	switch t {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune", "string":
		return lit
	}
	return t + "(" + lit + ")"
}

func intCandidates(p Param, h *Hint) []int64 {
	base := []int64{0, 1, -1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 31, 32, 63, 64, 127, 128, 255, 256, 1024, 65535, 65536, -2, -128, -129}
	for _, c := range h.Ints {
		base = append(base, c, c-1, c+1)
	}
	lo, hi := int64(math.MinInt64), int64(math.MaxInt64)
	if p.Kind == "int" && p.Bits < 64 {
		lo, hi = -(1 << (p.Bits - 1)), 1<<(p.Bits-1)-1
	}
	if p.Kind == "uint" {
		lo = 0
		if p.Bits < 64 {
			hi = 1<<p.Bits - 1
		}
	}
	base = append(base, hi, lo, hi-1, lo+1)
	var out []int64
	seen := map[int64]bool{}
	for _, v := range base {
		if v < lo || v > hi || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if p.Kind == "uint" {
		out = append(out, -1) // marks the type's maximum, see intLit
	}
	return out
}

func stringCandidates(h *Hint) []string {
	out := []string{"", "a", "\x00", "\xff", "aa", "ab", "a/b", "/", "//", "abc", " ", "\n", "0", "1", "-1", "\x80", "\xc2", "\xe2\x82", "\U0010ffff"}
	for _, s := range h.Strs {
		out = append(out, s)
		if len(s) > 0 {
			out = append(out, s[:len(s)-1], s+"a", "a"+s, s+s)
		}
	}
	for _, c := range h.Ints {
		if c > 0 && c < 256 {
			out = append(out, string([]byte{byte(c)}), string([]byte{byte(c), byte(c)}), "a"+string([]byte{byte(c)}))
		}
	}
	// Pairs of the function's byte constants, and each followed by a
	// space or a zero byte (a sign then the end, a tag then a length).
	var bs []byte
	for _, c := range h.Ints {
		if c > 0 && c < 256 && len(bs) < 8 {
			bs = append(bs, byte(c))
		}
	}
	for _, a := range bs {
		for _, b := range bs {
			out = append(out, string([]byte{a, b}))
		}
		out = append(out, string([]byte{a, ' '}), string([]byte{a, 0}), string([]byte{a, 0xff}))
	}
	for _, n := range []int{3, 4, 7, 8, 9, 15, 16, 17, 32, 64} {
		out = append(out, strings.Repeat("a", n), strings.Repeat("\x00", n), strings.Repeat("\xff", n))
	}
	// Long inputs of high bytes: lengths read from the input that wrap
	// in byte or uint16 arithmetic need more than 255 bytes.
	for _, n := range []int{127, 128, 129, 255, 256, 257, 258, 259, 260, 300, 1024} {
		out = append(out, strings.Repeat("\xff", n), strings.Repeat("\xfd", n))
	}
	return out
}

// combos picks the argument index tuples to run, deterministic from seed:
// every combination when there are few, else the small ones and a random
// sample.
func combos(sizes []int, limit int, seed uint64) [][]int {
	total := 1
	for _, n := range sizes {
		total *= n
		if total > limit {
			break
		}
	}
	var out [][]int
	if total <= limit {
		idx := make([]int, len(sizes))
		for {
			out = append(out, slices.Clone(idx))
			i := len(idx) - 1
			for ; i >= 0; i-- {
				idx[i]++
				if idx[i] < sizes[i] {
					break
				}
				idx[i] = 0
			}
			if i < 0 {
				break
			}
		}
		return out
	}
	seen := map[string]bool{}
	push := func(c []int) {
		k := fmt.Sprint(c)
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	// Small values of every parameter together.
	small := make([]int, len(sizes))
	for i := range small {
		small[i] = min(sizes[i], 4)
	}
	idx := make([]int, len(sizes))
	for len(out) < limit/2 {
		push(slices.Clone(idx))
		i := len(idx) - 1
		for ; i >= 0; i-- {
			idx[i]++
			if idx[i] < small[i] {
				break
			}
			idx[i] = 0
		}
		if i < 0 {
			break
		}
	}
	// Each value of each parameter with the others small.
	for p := range sizes {
		for v := 0; v < sizes[p] && len(out) < limit*3/4; v++ {
			c := make([]int, len(sizes))
			c[p] = v
			push(c)
		}
	}
	r := rand.New(rand.NewPCG(seed, 0x6d61726d6f74))
	for tries := 0; len(out) < limit && tries < 4*limit; tries++ {
		c := make([]int, len(sizes))
		for i := range c {
			c[i] = r.IntN(sizes[i])
		}
		push(c)
	}
	return out
}

// plan is the search of one package: per function its candidate
// literals and combinations.
type plan struct {
	hints  []*Hint
	cands  [][][]string // [fn][param][value]
	combos [][][]int    // [fn][combo][param]
}

func makePlan(hints []*Hint, limit int) *plan {
	p := &plan{hints: hints}
	for i, h := range hints {
		var cs [][]string
		var sz []int
		for _, pr := range h.Params {
			c := candidates(pr, h)
			cs = append(cs, c)
			sz = append(sz, len(c))
		}
		p.cands = append(p.cands, cs)
		p.combos = append(p.combos, combos(sz, limit, uint64(i)+1))
	}
	return p
}

// testSource is the generated in-package test that runs the plan: one
// function per searched function, selected by MARMOT_SEARCH (its index)
// and optionally MARMOT_INPUT (one combination). Each call recovers and
// prints the panic and the frame that raised it.
func (p *plan) testSource(pkgName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by marmot verify; DO NOT EDIT.\n\npackage %s\n\n", pkgName)
	b.WriteString(`import (
	marmotbytes "bytes"
	marmotfmt "fmt"
	marmotio "io"
	marmotos "os"
	marmotrt "runtime"
	marmotstrconv "strconv"
	marmottesting "testing"
)

var _ = marmotbytes.NewReader
var _ = marmotio.EOF

func marmotRun(fn, in int, f func()) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		pcs := make([]uintptr, 64)
		n := marmotrt.Callers(1, pcs)
		frames := marmotrt.CallersFrames(pcs[:n])
		file, line, past := "", 0, false
		for {
			fr, more := frames.Next()
			if fr.Function == "runtime.gopanic" {
				past = true
			} else if past && !(len(fr.Function) > 8 && fr.Function[:8] == "runtime.") {
				file, line = fr.File, fr.Line
				break
			}
			if !more {
				break
			}
		}
		marmotfmt.Printf("MARMOT\t%d\t%d\t%s\t%d\t%s\n", fn, in, file, line, marmotstrconv.Quote(marmotfmt.Sprint(r)))
	}()
	f()
}

func marmotSelected(t *marmottesting.T) (fn, in int) {
	fn, err := marmotstrconv.Atoi(marmotos.Getenv("MARMOT_SEARCH"))
	if err != nil {
		t.Skip("not a marmot verify search run")
	}
	in = -1
	if s := marmotos.Getenv("MARMOT_INPUT"); s != "" {
		in, _ = marmotstrconv.Atoi(s)
	}
	return fn, in
}

func TestMarmotVerifySearch(t *marmottesting.T) {
	fn, in := marmotSelected(t)
	switch fn {
`)
	for i, h := range p.hints {
		fmt.Fprintf(&b, "\tcase %d:\n\t\tmarmotSearch%d(%d, in)\n", i, i, i)
		_ = h
	}
	b.WriteString("\t}\n}\n")
	for i, h := range p.hints {
		fmt.Fprintf(&b, "\nfunc marmotSearch%d(fn, only int) {\n", i)
		for j, pr := range h.Params {
			fmt.Fprintf(&b, "\tp%d := []%s{%s}\n", j, pr.Type, strings.Join(p.cands[i][j], ", "))
		}
		var flat []string
		for _, c := range p.combos[i] {
			for _, x := range c {
				flat = append(flat, strconv.Itoa(x))
			}
		}
		fmt.Fprintf(&b, "\tcs := []uint16{%s}\n\t_ = cs\n", strings.Join(flat, ","))
		k := len(h.Params)
		callee := h.callee()
		if k == 0 {
			fmt.Fprintf(&b, "\tmarmotRun(fn, 0, func() { %s() })\n}\n", callee)
			continue
		}
		fmt.Fprintf(&b, "\tfor i := 0; i+%d <= len(cs); i += %d {\n", k, k)
		fmt.Fprintf(&b, "\t\tn := i / %d\n\t\tif only >= 0 && n != only {\n\t\t\tcontinue\n\t\t}\n", k)
		var args []string
		for j := range h.Params {
			args = append(args, fmt.Sprintf("p%d[cs[i+%d]]", j, j))
		}
		if h.Variadic {
			args[len(args)-1] += "..."
		}
		fmt.Fprintf(&b, "\t\tmarmotRun(fn, n, func() { %s(%s) })\n\t}\n}\n", callee, strings.Join(args, ", "))
	}
	return b.String()
}

// callee is the expression called: the function, or the method on a
// fresh zero value of its receiver type.
func (h *Hint) callee() string {
	switch {
	case h.Recv == "":
		return h.Fn
	case strings.HasPrefix(h.Recv, "*"):
		return "new(" + h.Recv[1:] + ")." + h.Fn
	}
	return "(*new(" + h.Recv + "))." + h.Fn
}

// Call is the call of h with args as Go source.
func (h *Hint) Call(args []string) string {
	if h.Variadic && len(args) > 0 {
		args = slices.Clone(args)
		args[len(args)-1] += "..."
	}
	c := h.Fn
	switch {
	case strings.HasPrefix(h.Recv, "*"):
		c = "new(" + h.Recv[1:] + ")." + h.Fn
	case h.Recv != "":
		c = h.Recv + "{}." + h.Fn
	}
	return c + "(" + strings.Join(args, ", ") + ")"
}

// API reports whether every caller can make this call: an exported
// function, or an exported method on an exported type's zero value.
func (h *Hint) API() bool {
	return h.Exported && (h.Recv == "" || h.RecvExported)
}

// Hit is a confirmed panic: the obligation it violates and the input.
type Hit struct {
	Fn           string
	Call         string // the call as Go source
	API          bool   // the call is possible from other packages
	Obligation   string
	Kind         string
	Input        map[string]string
	Args         []string
	PanicMessage string
	File         string
	Line         int
	Runs         int // of the confirmation runs, how many panicked there
}

// SearchOptions control the runs.
type SearchOptions struct {
	Limit   int           // combinations per function
	Timeout time.Duration // wall clock per function
	Sandbox string        // "off": no runs; anything else: runs in the sandbox
	// Effects also runs functions whose calls reach files, processes or
	// the network (in the sandbox all the same).
	Effects  bool
	Scratch  string // temporary directory root ("" = os.TempDir)
	GoBin    string // "go"
	Progress func(string)
}

// SandboxAvailable reports whether confirmation runs can be sandboxed
// here: sandbox-exec on macOS, bwrap on Linux (a read-only root with
// every namespace unshared). Without one nothing is run.
func SandboxAvailable() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("sandbox-exec")
		return err == nil
	case "linux":
		if _, err := exec.LookPath("bwrap"); err != nil {
			return false
		}
		return exec.Command("bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--unshare-all", "--die-with-parent", "true").Run() == nil
	}
	return false
}

// SandboxCommand returns the command that runs argv confined to work:
// no network, no writes outside work, and on macOS no new processes (on
// Linux children stay in the same confinement). The environment is
// cleared but for HOME and TMPDIR (work), PATH and env.
func SandboxCommand(ctx context.Context, work string, cpuSeconds int, env []string, argv ...string) (*exec.Cmd, error) {
	real, err := filepath.EvalSymlinks(work)
	if err != nil {
		return nil, err
	}
	cpu := strconv.Itoa(cpuSeconds)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", "ulimit -t " + cpu + "; exec sandbox-exec -p \"$0\" \"$@\"", sandboxProfile(real)}, argv...)...)
	case "linux":
		args := []string{"-c", "ulimit -t " + cpu + "; ulimit -v 4194304; exec bwrap --ro-bind / / --dev /dev --proc /proc --tmpfs /tmp --bind \"$0\" \"$0\" --chdir \"$0\" --unshare-all --die-with-parent --new-session \"$@\"", real}
		cmd = exec.CommandContext(ctx, "/bin/sh", append(args, argv...)...)
	default:
		return nil, fmt.Errorf("no sandbox on %s", runtime.GOOS)
	}
	cmd.Dir = real
	e := []string{"HOME=" + real, "TMPDIR=" + real}
	if p := os.Getenv("PATH"); p != "" {
		e = append(e, "PATH="+p)
	}
	cmd.Env = append(e, env...)
	cmd.WaitDelay = time.Second
	return cmd, nil
}

// Search builds the generated test for the package in dir and runs it
// for every hint, in a sandbox, then confirms each panic three times.
func Search(ctx context.Context, dir, pkgName string, hints []*Hint, opts SearchOptions) ([]Hit, error) {
	if len(hints) == 0 || opts.Sandbox == "off" {
		return nil, nil
	}
	if !SandboxAvailable() {
		return nil, fmt.Errorf("no sandbox available (sandbox-exec on macOS, bwrap on Linux): nothing was run")
	}
	if opts.Limit == 0 {
		opts.Limit = 2000
	}
	if opts.Timeout == 0 {
		opts.Timeout = 3 * time.Second
	}
	if opts.GoBin == "" {
		opts.GoBin = "go"
	}
	tmp, err := os.MkdirTemp(opts.Scratch, "marmot-verify-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if !opts.Effects {
		var keep []*Hint
		for _, h := range hints {
			if !h.Effects {
				keep = append(keep, h)
			}
		}
		hints = keep
		if len(hints) == 0 {
			return nil, nil
		}
	}
	p := makePlan(hints, opts.Limit)
	src := filepath.Join(tmp, "zz_marmot_verify_search_test.go")
	if err := os.WriteFile(src, []byte(p.testSource(pkgName)), 0o644); err != nil {
		return nil, err
	}
	ov, _ := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(dir, "zz_marmot_verify_search_test.go"): src}})
	ovFile := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(ovFile, ov, 0o644); err != nil {
		return nil, err
	}
	bin := filepath.Join(tmp, "search.test")
	build := exec.CommandContext(ctx, opts.GoBin, "test", "-c", "-o", bin, "-overlay", ovFile, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("building the search test: %v: %s", err, firstLines(string(out), 5))
	}
	run := func(fn, in int) []rawHit {
		work := filepath.Join(tmp, fmt.Sprintf("run-%d", fn))
		_ = os.MkdirAll(work, 0o755)
		out := runSandboxed(ctx, opts, bin, work, fn, in)
		return parseHits(out)
	}
	var hits []Hit
	for i, h := range hints {
		if opts.Progress != nil {
			opts.Progress("verify: searching " + h.Full)
		}
		raws := run(i, -1)
		// One hit per obligation: the first input that reaches it.
		done := map[string]bool{}
		for _, r := range raws {
			o := matchObligation(h, r)
			if o == nil || done[o.Key] {
				continue
			}
			done[o.Key] = true
			// Confirm: the same input three times.
			n := 0
			for k := 0; k < 3; k++ {
				for _, r2 := range run(i, r.in) {
					if r2.file == r.file && r2.line == r.line {
						n++
						break
					}
				}
			}
			args := make([]string, len(h.Params))
			input := map[string]string{}
			for j, pr := range h.Params {
				args[j] = p.cands[i][j][p.combos[i][r.in][j]]
				input[pr.Name] = args[j]
			}
			hits = append(hits, Hit{Fn: h.Full, Call: h.Call(args), API: h.API(), Obligation: o.Key, Kind: o.Kind, Input: input, Args: args, PanicMessage: r.msg, File: r.file, Line: r.line, Runs: n})
		}
	}
	return hits, nil
}

type rawHit struct {
	fn, in int
	file   string
	line   int
	msg    string
}

func parseHits(out []byte) []rawHit {
	var hs []rawHit
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 6 || f[0] != "MARMOT" {
			continue
		}
		fn, _ := strconv.Atoi(f[1])
		in, _ := strconv.Atoi(f[2])
		line, _ := strconv.Atoi(f[4])
		msg, err := strconv.Unquote(f[5])
		if err != nil {
			msg = f[5]
		}
		hs = append(hs, rawHit{fn, in, f[3], line, msg})
	}
	return hs
}

// matchObligation finds the open obligation a panic belongs to: same
// file and line, and a kind that fits the message.
func matchObligation(h *Hint, r rawHit) *OblRef {
	var cands []*OblRef
	for i := range h.Obls {
		o := &h.Obls[i]
		if o.Line == r.line && sameFile(o.File, r.file) {
			cands = append(cands, o)
		}
	}
	want := kindOfMessage(r.msg)
	for _, o := range cands {
		if o.Kind == want {
			return o
		}
	}
	return nil
}

func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// kindOfMessage maps a run-time panic message to an obligation kind.
func kindOfMessage(msg string) string {
	switch {
	case strings.Contains(msg, "index out of range"):
		return string(KIndex)
	case strings.Contains(msg, "slice bounds out of range"):
		return string(KSlice)
	case strings.Contains(msg, "nil pointer dereference"), strings.Contains(msg, "invalid memory address"):
		return string(KNil)
	case strings.Contains(msg, "integer divide by zero"):
		return string(KDivide)
	case strings.Contains(msg, "negative shift amount"):
		return string(KShift)
	case strings.Contains(msg, "makeslice"), strings.Contains(msg, "makechan"):
		return string(KMake)
	case strings.Contains(msg, "interface conversion"):
		return string(KAssert)
	case strings.Contains(msg, "assignment to entry in nil map"):
		return string(KMap)
	case strings.Contains(msg, "cannot convert slice"):
		return string(KConvert)
	case strings.Contains(msg, "comparing uncomparable"), strings.Contains(msg, "hash of unhashable"):
		return string(KCompare)
	}
	return string(KPanic)
}

// sandboxProfile denies the network, every write outside work and new
// processes.
func sandboxProfile(work string) string {
	return fmt.Sprintf(`(version 1)
(allow default)
(deny network*)
(deny process-fork)
(deny file-write*)
(allow file-write* (subpath %q) (literal "/dev/null") (literal "/dev/stdout") (literal "/dev/stderr") (literal "/dev/dtracehelper"))
`, work)
}

// runSandboxed runs one search process in the sandbox with a CPU limit
// and a wall clock kill. A run that cannot be sandboxed does not happen.
func runSandboxed(ctx context.Context, opts SearchOptions, bin, work string, fn, in int) []byte {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	env := []string{"GOMEMLIMIT=1GiB", "GOTRACEBACK=none", "MARMOT_SEARCH=" + strconv.Itoa(fn)}
	if in >= 0 {
		env = append(env, "MARMOT_INPUT="+strconv.Itoa(in))
	}
	cmd, err := SandboxCommand(ctx, work, int(opts.Timeout.Seconds())+1, env, bin, "-test.run=^TestMarmotVerifySearch$", "-test.count=1")
	if err != nil {
		return nil
	}
	out, _ := cmd.Output()
	return out
}

func firstLines(s string, n int) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	if len(ls) > n {
		ls = ls[:n]
	}
	return strings.Join(ls, "; ")
}

// KindText is the panic of an obligation kind in words.
func KindText(kind string) string {
	return panicText(&Obligation{Kind: Kind(kind)})
}
