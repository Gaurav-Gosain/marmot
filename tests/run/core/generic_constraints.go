package main

type Stringish interface{ ~string }

type Ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~float32 | ~float64 | ~string
}

type Shower interface{ Show() string }

type Num int

func (n Num) Show() string { return "num" }

func ShowAll[T Shower](xs ...T) string {
	r := ""
	for _, x := range xs {
		r += x.Show()
	}
	return r
}

func Len[S Stringish](s S) int { return len(s) }

func Min[T Ordered](a T, rest ...T) T {
	m := a
	for _, r := range rest {
		if r < m {
			m = r
		}
	}
	return m
}

func Keys[M ~map[K]V, K comparable, V any](m M) int { return len(m) }

func Ptr[T any](v T) *T { return &v }

func Zero[T any]() T {
	var z T
	return z
}

func Eq[T comparable](a, b T) bool { return a == b }

type Name string

func main() {
	println(ShowAll(Num(1), Num(2)))
	println(Len("abc"), Len(Name("hello")))
	println(Min(3, 1, 2), Min("b", "a"), Min(2.5), Min[int8](5, -5))
	println(Keys(map[string]int{"a": 1}))
	p := Ptr(5)
	*p++
	println(*p, *Ptr("s"))
	println(Zero[int](), Zero[string]() == "", Zero[*int]() == nil, Zero[[2]int]()[1])
	println(Eq(1, 1), Eq("a", "b"), Eq(1.5, 1.5), Eq[any](1, "1"))
}
