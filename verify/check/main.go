// Command check re-checks go-bce on real compiler output, independently of LLVM and of the
// backend: every check the pass removed must be justified by a rule of docs/passes.md as
// proved in verify/bend.
//
//	check -before in.mlir -after out.mlir
//
// in.mlir is the go dialect MLIR the frontend emits (marmot build -dialect=go -emit mlir),
// out.mlir is `marmot-opt --go-bce in.mlir`. go.index checks are matched one to one (the
// pass only sets nocheck); go.bounds_check and go.nil_check ops the pass erased are found
// per block by matching the remaining ones on a renaming-free description of their
// operands. Exit status 1 when a removal has no justification, 2 on a usage or read error.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	before := flag.String("before", "", "go dialect MLIR before go-bce")
	after := flag.String("after", "", "the same after go-bce")
	verbose := flag.Bool("v", false, "print every justified removal")
	flag.Parse()
	if *before == "" || *after == "" {
		flag.Usage()
		os.Exit(2)
	}
	rs, err := CheckFiles(*before, *after)
	if err != nil {
		fmt.Fprintln(os.Stderr, "check:", err)
		os.Exit(2)
	}
	bad := 0
	for _, r := range rs {
		if r.Rule == "" {
			bad++
			fmt.Println(r)
		} else if *verbose {
			fmt.Println(r)
		}
	}
	fmt.Printf("check: %d removals, %d without justification\n", len(rs), bad)
	if bad > 0 {
		os.Exit(1)
	}
}

func readFuncs(path string) (map[string]*Func, []string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer fh.Close()
	fns, err := Parse(fh)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", path, err)
	}
	m := map[string]*Func{}
	var order []string
	for _, f := range fns {
		m[f.Name] = f
		order = append(order, f.Name)
	}
	return m, order, nil
}

// CheckFiles marks the removed checks of every function and justifies each.
func CheckFiles(beforePath, afterPath string) ([]Result, error) {
	bf, order, err := readFuncs(beforePath)
	if err != nil {
		return nil, err
	}
	af, _, err := readFuncs(afterPath)
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, name := range order {
		f, g := bf[name], af[name]
		if g == nil {
			return nil, fmt.Errorf("function %s missing after go-bce", name)
		}
		if len(f.Blocks) != len(g.Blocks) {
			return nil, fmt.Errorf("%s: block count changed (%d -> %d)", name, len(f.Blocks), len(g.Blocks))
		}
		cs, ds := checksOf(f), checksOf(g)
		if err := markRemoved(f, g, cs, ds); err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		d := newDom(f)
		var kept []*Check
		for _, c := range cs {
			if !c.Removed {
				kept = append(kept, c)
			}
		}
		for _, c := range cs {
			if !c.Removed {
				continue
			}
			rule, why := Justify(f, d, c, kept)
			out = append(out, Result{Func: name, Rule: rule, Why: why, Check: c})
		}
	}
	return out, nil
}

func markRemoved(f, g *Func, cs, ds []*Check) error {
	// go.index: one to one, in walk order
	var ci, di []*Check
	for _, c := range cs {
		if c.Kind == "index" {
			if c.Removed {
				return fmt.Errorf("input already has nocheck indexing")
			}
			ci = append(ci, c)
		}
	}
	for _, c := range ds {
		if c.Kind == "index" {
			di = append(di, c)
		}
	}
	if len(ci) != len(di) {
		return fmt.Errorf("go.index count changed (%d -> %d)", len(ci), len(di))
	}
	for k := range ci {
		ci[k].Removed = di[k].Removed
	}
	// go.bounds_check, go.nil_check: per block, the remaining ones are a subsequence
	for bi := range f.Blocks {
		var bs, as []*Check
		for _, c := range cs {
			if c.Kind != "index" && c.Op.Block == f.Blocks[bi] {
				bs = append(bs, c)
			}
		}
		for _, c := range ds {
			if c.Kind != "index" && c.Op.Block == g.Blocks[bi] {
				as = append(as, c)
			}
		}
		j := 0
		for _, a := range as {
			sa := sig(g, a)
			for j < len(bs) && sig(f, bs[j]) != sa {
				bs[j].Removed = true
				j++
			}
			if j == len(bs) {
				return fmt.Errorf("block %s: cannot match the remaining %s check %s", f.Blocks[bi].Name, a.Kind, sa)
			}
			j++
		}
		for ; j < len(bs); j++ {
			bs[j].Removed = true
		}
	}
	return nil
}

func sig(f *Func, c *Check) string {
	if c.Kind == "nil" {
		return "nil(" + desc(f, c.P, 3) + ")"
	}
	return c.Kind + "(" + desc(f, c.I, 3) + "," + desc(f, c.N, 3) + ")"
}

// desc describes a value without its SSA name, so that the two printings agree.
func desc(f *Func, v string, depth int) string {
	if b := f.ArgOf[v]; b != nil {
		return fmt.Sprintf("arg%d.%d", b.Index, f.ArgIdx[v])
	}
	if c, t, ok := f.Const(v); ok {
		return fmt.Sprintf("%d:%s", c, t)
	}
	op := f.Def[v]
	if op == nil {
		if i, ok := f.FArg[v]; ok {
			return fmt.Sprintf("farg%d", i)
		}
		return "?"
	}
	if depth == 0 {
		return op.Name
	}
	var parts []string
	for _, o := range op.Operands {
		parts = append(parts, desc(f, o, depth-1))
	}
	extra := ""
	if op.Name == "llvm.extractvalue" {
		if m := reEVPos.FindStringSubmatch(op.Text); m != nil {
			extra = "[" + m[1] + "]"
		}
	}
	// Ordinal among the ops of the same name in the block, so that two calls of one
	// function are told apart. go-bce inserts llvm.icmp (for llvm.intr.assume) and erases
	// only checks, so other ordinals are the same in both printings.
	if op.Name != "llvm.icmp" {
		k := 0
		for _, o := range op.Block.Ops[:op.Pos] {
			if o.Name == op.Name {
				k++
			}
		}
		extra += fmt.Sprintf("#%d", k)
	}
	return fmt.Sprintf("%s@%d%s(%s)", op.Name, op.Block.Index, extra, strings.Join(parts, ","))
}
