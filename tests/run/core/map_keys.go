package main

type P struct {
	X, Y int
}
type N struct {
	A string
	B [2]int8
	C P
}

func main() {
	i8 := map[int8]int{-1: 1, 127: 2}
	println(i8[-1], i8[127], i8[0])
	u64 := map[uint64]int{1 << 63: 1}
	println(u64[1<<63])
	f := map[float64]string{1.5: "a", -0.5: "b"}
	println(f[1.5], f[-0.5])
	nan := 0.0
	nan = nan / nan
	fm := map[float64]int{}
	fm[nan] = 1
	fm[nan] = 2
	println(len(fm), fm[nan])
	b := map[bool]int{true: 1}
	b[false] = 2
	println(b[true], b[false])
	s := map[string][]int{"k": {1, 2}}
	s["k"] = append(s["k"], 3)
	println(len(s["k"]))
	st := map[P]string{{1, 2}: "a"}
	st[P{3, 4}] = "b"
	println(st[P{1, 2}], st[P{3, 4}], st[P{9, 9}] == "")
	n := map[N]int{}
	n[N{"x", [2]int8{1, 2}, P{1, 1}}] = 5
	println(n[N{"x", [2]int8{1, 2}, P{1, 1}}], n[N{"x", [2]int8{1, 3}, P{1, 1}}])
	ar := map[[3]int]int{{1, 2, 3}: 7}
	println(ar[[3]int{1, 2, 3}])
	pt := map[*int]int{}
	x, y := 1, 1
	pt[&x] = 1
	pt[&y] = 2
	println(len(pt), pt[&x], pt[&y])
	c := map[complex128]int{1 + 2i: 5}
	println(c[1+2i])
	r := map[rune]int{'a': 1}
	println(r['a'])
	var ch1 = make(chan int)
	cm := map[chan int]int{ch1: 3}
	println(cm[ch1])
}
