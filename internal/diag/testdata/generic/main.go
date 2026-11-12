package main

import "fmt"

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

type Celsius float32

func main() {
	fmt.Println(Sum([]string{"a"}))
	fmt.Println(Sum([]Celsius{1}))
}
