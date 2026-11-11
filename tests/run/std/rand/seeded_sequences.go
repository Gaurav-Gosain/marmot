package main

import "math/rand"

func main() {
	for _, seed := range []int64{0, 1, 2, -5, 1 << 40} {
		r := rand.New(rand.NewSource(seed))
		println(seed, r.Int63(), r.Int31(), r.Intn(1000), r.Uint64(), r.Float32())
	}
	r := rand.New(rand.NewSource(99))
	r.Seed(5)
	println(r.Intn(10), r.Intn(10), r.Intn(10))
	r2 := rand.New(rand.NewSource(5))
	println(r2.Intn(10), r2.Intn(10), r2.Intn(10))
}
