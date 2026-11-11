// reflectgc: types built at run time by reflect (StructOf, ArrayOf, MapOf, SliceOf), whose
// pointer bitmaps the reflect overlay computes; the only references to fresh objects are in
// their pointer fields across forced collections.
package main

import (
	"fmt"
	"reflect"
	"runtime"
)

type val struct{ n int }

//go:noinline
func mk(n int) *val { return &val{n} }

func gc() {
	for i := 0; i < 3; i++ {
		runtime.GC()
		junk := make([]*val, 256)
		for j := range junk {
			junk[j] = &val{-1}
		}
		_ = junk
	}
}

func main() {
	pv := reflect.TypeOf((*val)(nil))
	st := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeOf(byte(0))},
		{Name: "P", Type: pv},
		{Name: "Pad", Type: reflect.TypeOf([5]int{})},
		{Name: "Q", Type: pv},
		{Name: "Z", Type: reflect.TypeOf(struct{}{})},
	})
	at := reflect.ArrayOf(3, st)
	mt := reflect.MapOf(reflect.TypeOf(""), pv)
	slt := reflect.SliceOf(pv)

	s := reflect.New(st).Elem()
	s.Field(1).Set(reflect.ValueOf(mk(1)))
	s.Field(3).Set(reflect.ValueOf(mk(2)))
	a := reflect.New(at).Elem()
	a.Index(2).Field(3).Set(reflect.ValueOf(mk(3)))
	m := reflect.MakeMap(mt)
	m.SetMapIndex(reflect.ValueOf("k"), reflect.ValueOf(mk(4)))
	sl := reflect.MakeSlice(slt, 0, 1)
	for i := 0; i < 40; i++ {
		sl = reflect.Append(sl, reflect.ValueOf(mk(5+i)))
	}
	gc()
	fmt.Println(st.Size(), s.Field(1).Interface().(*val).n, s.Field(3).Interface().(*val).n,
		a.Index(2).Field(3).Interface().(*val).n, m.MapIndex(reflect.ValueOf("k")).Interface().(*val).n,
		sl.Index(39).Interface().(*val).n)
}
