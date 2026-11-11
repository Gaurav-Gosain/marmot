package main

func main() {
	a := make([]int, 50)
	for i := range a {
		a[i] = i
	}
	for _, step := range []int{1, 2, 3, 7, 16, 49, 50, 51} {
		s := 0
		for i := 0; i < len(a); i += step {
			s += a[i]
		}
		println(step, s)
	}
	for _, lo := range []int{0, 1, 49, 50} {
		s := 0
		for i := len(a) - 1; i >= lo; i -= 3 {
			s = s*3 + a[i]
		}
		println(lo, s)
	}
	b := a[10:40:45]
	s := 0
	for i := range b {
		s += b[i] * i
	}
	println(s, len(b), cap(b))
	m := [4][4]int{}
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			m[i][j] = i*4 + j
		}
	}
	t := 0
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			t = t*2 + m[i][j]
		}
	}
	println(t)
}
