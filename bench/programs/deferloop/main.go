// deferloop: defer in a hot loop. Each iteration depends on the state left by the previous one
// through its deferred calls, so the loop cannot be folded to a closed form.
package main

const iters = 200000000

var state uint64

func add(p *uint64, d uint64) { *p += d }

func step(i int) {
	v := uint64(i)*2654435761 ^ state
	defer func() { state = state*6364136223846793005 + v }()
	defer add(&v, state>>7)
	v ^= v >> 3
}

func main() {
	for i := 0; i < iters; i++ {
		step(i)
	}
	println("deferloop", state)
}
