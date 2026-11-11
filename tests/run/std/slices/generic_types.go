package main

import "slices"

type ID int
type IDs []ID

func main() {
	ids := IDs{3, 1, 2}
	slices.Sort(ids)
	println(ids[0], ids[2], slices.Index(ids, 2))
	c := slices.Clone(ids)
	c[0] = 99
	println(ids[0], c[0])
	f := []float64{2.5, 1.5}
	slices.Sort(f)
	println(f[0])
	type pt struct{ x, y int }
	ps := []pt{{1, 2}, {1, 2}}
	println(slices.Equal(ps, []pt{{1, 2}, {1, 2}}), slices.Index(ps, pt{1, 2}))
	bs := [][]byte{[]byte("a")}
	println(slices.EqualFunc(bs, [][]byte{[]byte("a")}, func(x, y []byte) bool { return string(x) == string(y) }))
}
