package main

import "math/rand"

func main() {
	r := rand.New(rand.NewSource(42))
	println(r.Intn(100), r.Intn(100), r.Intn(100), r.Int63(), r.Float64())
	r2 := rand.New(rand.NewSource(42))
	println(r2.Intn(100), r2.Intn(100))
	p := r.Perm(5)
	println(p[0], p[1], p[2], p[3], p[4])
	a := []int{1, 2, 3, 4, 5, 6}
	r.Shuffle(len(a), func(i, j int) { a[i], a[j] = a[j], a[i] })
	println(a[0], a[1], a[2], a[3], a[4], a[5])
	println(r.Int31n(10), r.Uint32(), r.NormFloat64(), r.ExpFloat64())
}
