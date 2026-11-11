// defer in a hot loop: a small function that defers an update, called many times.
package main

const iters = 200000000

var total int

func add(p *int, v int) { *p += v }

func step(i int) {
	local := 0
	defer add(&local, i)
	defer func() { total += local }()
	local++
}

func main() {
	for i := 0; i < iters; i++ {
		step(i)
	}
	println("deferloop", total)
}
