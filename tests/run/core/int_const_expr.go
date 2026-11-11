package main

const (
	A = 1 << 10
	B = A * A
	C = B * B * B
	D = C / (A * A * A * A)
	E = 'a' + 1
	F = 7 / 2
	G = 7.0 / 2
	H = -7 % 3
)

func main() {
	println(A, B, D, E, F, H)
	println(G)
	var x uint64 = C >> 8
	println(x)
	const huge = 1 << 100
	println(huge >> 98)
	println(huge / (1 << 90))
	var f float64 = huge
	println(f)
}
