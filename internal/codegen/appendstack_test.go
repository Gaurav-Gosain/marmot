package codegen

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// The append sites that get a frame buffer (MARMOT_APPENDSTACK=1): nil
// operand or a loop phi from nil, and no derived value leaves the function.
// The rejected cases are the review's adversarial programs: a copy kept
// from an earlier iteration, a goto loop that keeps every slice, appends at
// other sites, a deferred closure, a returned closure, a call argument.
func TestAppendStackSites(t *testing.T) {
	const src = `package p

import "strings"

func straight(x, y int) int { s := append([]int(nil), x, y); return s[0] + s[1] + cap(s) }

func loop() int {
	var s []int
	for i := 0; i < 3; i++ {
		s = append(s, i)
	}
	s[1] = 5
	n := 0
	for _, v := range s {
		n += v
	}
	return n + len(s[1:])
}

func aliasLoop() int {
	var s, prev []int
	for i := 0; i < 2; i++ {
		prev = s
		s = nil
		s = append(s, i)
	}
	return prev[0] + s[0]
}

func gotoLoop() int {
	var keep [][]int
	i := 0
again:
	var s []int
	s = append(s, i)
	keep = append(keep, s)
	i++
	if i < 3 {
		goto again
	}
	return keep[0][0]
}

func otherSite() int {
	var s []byte
	s = append(s, 1)
	t := append(s, 2)
	return int(t[1])
}

func deferRead() (r int) {
	var s []int
	s = append(s, 7)
	defer func() { r = s[0] }()
	return 0
}

func capture() func() int {
	var s []int
	s = append(s, 9)
	return func() int { return s[0] }
}

func passed() string {
	var parts []string
	for _, w := range []string{"a", "b"} {
		parts = append(parts, w)
	}
	return strings.Join(parts, "+")
}

func returned() []int { var s []int; s = append(s, 1); return s }

func stored(dst *[]int) { var s []int; s = append(s, 1); *dst = s }

func elemAddr() *int { var s []int; s = append(s, 1); return &s[0] }

func copied(dst []int) int { var s []int; s = append(s, 1, 2); return copy(dst, s) }

func loopTail(line string) int {
	var f []int
	for i := 0; i < len(line); i++ {
		f = append(f, i)
	}
	f = append(f, len(line))
	return len(f) + f[0]
}

func perIteration(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		s := append([]int(nil), i, i)
		sum += s[0] + s[1]
	}
	return sum
}

func resetKeep(c bool) int {
	s := append([]int(nil), 1)
	t := s
	if c {
		s = nil
	}
	s = append(s, 2)
	return t[0] + s[0]
}

func siblings(xs []int) int {
	s0 := append([]int(nil), xs...)
	a := append(s0, 1)
	b := append(s0, 2)
	return a[0] + b[0]
}

func keptAcross(n int) int {
	var old []int
	for i := 0; i < n; i++ {
		s := append([]int(nil), i)
		if i == 0 {
			old = s
		}
	}
	return len(old)
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, types.NewPackage("p", "p"), []*ast.File{f}, ssa.SanityCheckFunctions)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range pkg.Members {
		fn, ok := m.(*ssa.Function)
		if !ok {
			continue
		}
		groups, sites := 0, 0
		done := map[*ssa.Call]bool{}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				c, ok := instr.(*ssa.Call)
				if !ok || !isAppendSite(c) || done[c] {
					continue
				}
				if g := appendGroupOf(c); g != nil && appendStackSafe(g) {
					groups++
					sites += len(g.sites)
					for _, s := range g.sites {
						done[s] = true
					}
				}
			}
		}
		if groups > 0 {
			got = append(got, fmt.Sprintf("%s:%d/%d", fn.Name(), groups, sites))
		}
	}
	sort.Strings(got)
	// name:groups/sites. gotoLoop: keep's site qualifies (keep stays in
	// the function); the site of s does not (s is stored into keep).
	// otherSite: t := append(s, 2) shares s's store, as on the heap.
	// Rejected: aliasLoop and keptAcross (a slice kept from an earlier
	// iteration), resetKeep (two nil entries: a second take while t points
	// into the buffer), siblings (two sites grow the same empty value).
	if want := "copied:1/1 gotoLoop:1/1 loop:1/1 loopTail:1/2 otherSite:1/2 perIteration:1/1 straight:1/1"; strings.Join(got, " ") != want {
		t.Errorf("sites in %q, want %q", strings.Join(got, " "), want)
	}
}
