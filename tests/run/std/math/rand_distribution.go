package main

import "math/rand"

func main() {
	r := rand.New(rand.NewSource(7))
	var buckets [10]int
	for i := 0; i < 100000; i++ {
		buckets[r.Intn(10)]++
	}
	ok := true
	for _, b := range buckets {
		if b < 9000 || b > 11000 {
			ok = false
		}
	}
	println(ok)
	sum := 0.0
	for i := 0; i < 10000; i++ {
		f := r.Float64()
		if f < 0 || f >= 1 {
			println("range")
		}
		sum += f
	}
	println(sum > 4800 && sum < 5200)
	g := rand.New(rand.NewSource(1))
	println(g.Int63n(1<<40) < 1<<40)
}
