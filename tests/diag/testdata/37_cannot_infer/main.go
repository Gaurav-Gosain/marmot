package main

func Zero[T any]() T {
	var z T
	return z
}

func main() {
	x := Zero()
	_ = x
}
