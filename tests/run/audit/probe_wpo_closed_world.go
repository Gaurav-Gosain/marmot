// Audit probe (loops, 2026-11-12): held on HEAD 86cadb6 in dev, -release and
// GC stress. Kept as a guard; see docs/audit-backend.md.
package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type I interface{ Rare(x uint8, y [5]int8) [2]int32 }

type impl struct{ k int32 }

func (m impl) Rare(x uint8, y [5]int8) [2]int32 { return [2]int32{m.k + int32(x), int32(y[4])} }

//go:noinline
func callMethodExpr(f func(impl, uint8, [5]int8) [2]int32) [2]int32 {
	return f(impl{10}, 2, [5]int8{0, 0, 0, 0, 9})
}

func main() {
	// A method value made by reflect.
	v := reflect.ValueOf(impl{5}).MethodByName("Rare")
	g := v.Interface().(func(uint8, [5]int8) [2]int32)
	fmt.Println(g(1, [5]int8{0, 0, 0, 0, 7}))

	// Method expression.
	fmt.Println(callMethodExpr(impl.Rare))

	// A method value through an interface.
	var i I = impl{3}
	h := i.Rare
	fmt.Println(h(4, [5]int8{0, 0, 0, 0, 1}))

	// Standard library func values.
	s := []string{"b", "c", "a"}
	sort.Slice(s, func(x, y int) bool { return s[x] < s[y] })
	fmt.Println(s, strings.Map(func(r rune) rune { return r + 1 }, "HAL"))
}
