package main

func Size[T any]() uintptr {
	var z T
	_ = z
	return 0
}

func Swap[T any](a, b *T) { *a, *b = *b, *a }

func Apply[T any](x T, fs ...func(T) T) T {
	for _, f := range fs {
		x = f(x)
	}
	return x
}

type Big struct{ a, b, c, d int }

func Id[T any](v T) T { return v }

func main() {
	a, b := 1, 2
	Swap(&a, &b)
	println(a, b)
	s1, s2 := "x", "y"
	Swap(&s1, &s2)
	println(s1, s2)
	b1, b2 := Big{1, 2, 3, 4}, Big{5, 6, 7, 8}
	Swap(&b1, &b2)
	println(b1.a, b2.d)
	println(Apply(2, func(i int) int { return i * 3 }, func(i int) int { return i + 1 }))
	println(Apply("a", func(s string) string { return s + s }))
	println(Id(Big{9, 0, 0, 1}).a, Id[any](nil) == nil, Id(1.5), Id([]int{4})[0])
	println(Id(func() int { return 3 })())
	var e error
	println(Id(e) == nil)
}
