package mathx

type Number interface {
	~int | ~int64 | ~float64
}

func Sum[T Number](xs []T) T {
	var t T
	for _, x := range xs {
		t += x
	}
	return t
}
