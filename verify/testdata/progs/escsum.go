// escsum: interprocedural escape summaries and the extended escape rules (MARMOT_ESCAPE_SUMMARY,
// MARMOT_XPKG_ESCAPE, MARMOT_ESCAPE_EXT, MARMOT_STACKCONV). Each case lets a pointer leave through a
// callee; if it were wrongly put in the frame, later calls overwrite it.
package main

import (
	"fmt"
	"strings"
	"sync"
)

type T struct{ v [4]int }

var g *T
var gs []*T
var gstr string
var gb []byte

//go:noinline
func deep3(p *T) { g = p }

//go:noinline
func deep2(p *T) { deep3(p) }

//go:noinline
func deep1(p *T) { deep2(p) }

//go:noinline
func ident(p *T) *T { return p }

//go:noinline
func keepInClosure(p *T) func() int { return func() int { return p.v[0] } }

//go:noinline
func viaGoroutine(p *T, wg *sync.WaitGroup) {
	go func() { gs = append(gs, p); wg.Done() }()
}

//go:noinline
func viaVariadic(ps ...*T) { gs = append(gs, ps...) }

//go:noinline
func viaPanic(p *T) (r *T) {
	defer func() { r = recover().(*T) }()
	panic(p)
}

//go:noinline
func viaMethodValue(p *T) func() int { return p.get }

func (t *T) get() int { return t.v[0] }

//go:noinline
func viaDefer(p *T) {
	defer func() { g = p }()
}

//go:noinline
func viaString(b []byte) { gstr = string(b) }

//go:noinline
func viaBytes(s string) { gb = []byte(s) }

//go:noinline
func clobber() int {
	var a [64]int
	for i := range a {
		a[i] = -1
	}
	s := 0
	for _, x := range a {
		s += x
	}
	return s
}

//go:noinline
func run() (out []int) {
	{
		x := &T{v: [4]int{1}}
		deep1(x)
	}
	clobber()
	out = append(out, g.v[0])
	y := ident(&T{v: [4]int{2}})
	clobber()
	out = append(out, y.v[0])
	f := keepInClosure(&T{v: [4]int{3}})
	clobber()
	out = append(out, f())
	var wg sync.WaitGroup
	wg.Add(1)
	viaGoroutine(&T{v: [4]int{4}}, &wg)
	wg.Wait()
	clobber()
	out = append(out, gs[0].v[0])
	viaVariadic(&T{v: [4]int{5}})
	clobber()
	out = append(out, gs[1].v[0])
	r := viaPanic(&T{v: [4]int{6}})
	clobber()
	out = append(out, r.v[0])
	m := viaMethodValue(&T{v: [4]int{7}})
	clobber()
	out = append(out, m())
	viaDefer(&T{v: [4]int{8}})
	clobber()
	out = append(out, g.v[0])
	b := []byte("nine")
	viaString(b)
	b[0] = 'X'
	clobber()
	out = append(out, len(gstr), int(gstr[0]))
	viaBytes(strings.Repeat("t", 3))
	clobber()
	out = append(out, int(gb[0]))
	return
}

func main() { fmt.Println(run()) }
