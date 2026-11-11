package main

import "math/rand"

func main() {
	r := rand.New(rand.NewSource(11))
	b := make([]byte, 10)
	n, _ := r.Read(b)
	println(n)
	for _, x := range b {
		print(x, " ")
	}
	println()
	println(r.Int63n(100), r.Int31n(100), r.NormFloat64(), r.ExpFloat64())
	z := rand.NewZipf(r, 1.5, 2, 100)
	println(z.Uint64(), z.Uint64(), z.Uint64())
	println(rand.Intn(10) < 10, rand.Float64() < 1)
}
