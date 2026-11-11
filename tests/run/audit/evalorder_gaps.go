// Audit (spec, 2026-11-12): variable reads that gc performs after the calls
// of the statement and marmot performs before them. The evaluation order
// pass moves a load only when a call sits between the load and its first
// use; in each case below the load first feeds a lookup, an append, a call
// argument, a literal element, an addition or an index before the call.
// Not caused by the pass (MARMOT_NO_EVALORDER=1 gives the same values).
package main

import "fmt"

var g int
var gm = map[string]int{"k": 1}

func setg() int { g = 100; return 1 }
func setm() int { gm["k"] = 40; return 4 }
func bump(p *int) int {
	*p += 10
	return 2
}

func main() {
	h := gm["k"] + setm()
	fmt.Println("map index", h)

	s := []int{1}
	s = append(s, func() int { s = append(s, 8); s[0] = 7; return 2 }())
	fmt.Println("append", s)

	z := 3
	pz := &z
	r1, r2 := z, bump(pz)
	fmt.Println("multi", r1, r2, z)

	g = 1
	fmt.Println("args", g, setg(), g)

	g = 1
	lit := []int{g, setg(), g}
	fmt.Println("slice literal", lit)

	ch := make(chan int, 1)
	ch <- 1
	g = 1
	u := g + <-ch + setg()
	fmt.Println("receive between", u)

	var ifc any = 3
	g = 1
	o := g + ifc.(int) + setg()
	fmt.Println("assertion between", o)

	str := "abc"
	ps := &str
	sb := (*ps)[0] + byte(func() int { *ps = "xyz"; return 0 }())
	fmt.Println("string index", sb)
}
