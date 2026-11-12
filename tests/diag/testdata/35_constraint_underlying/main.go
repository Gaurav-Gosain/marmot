package main

type Number interface {
	~int | ~float64
}

func Max[T Number](a, b T) T {
	if a > b {
		return a
	}
	return b
}

type Meters float32

func main() {
	println(Max(Meters(1), Meters(2)))
}
