// struct copy heavy code: large value structs passed and returned by value.
package main

const iters = 80000000

type vec struct{ a, b, c, d, e, f, g, h int64 }

type state struct {
	pos, vel vec
	id       int64
	tag      [4]int64
}

func addv(x, y vec) vec {
	return vec{x.a + y.a, x.b + y.b, x.c + y.c, x.d + y.d, x.e + y.e, x.f + y.f, x.g + y.g, x.h + y.h}
}

func step(s state) state {
	s.pos = addv(s.pos, s.vel)
	s.vel.a ^= s.pos.h
	s.id++
	s.tag[s.id&3] += s.pos.a
	return s
}

func main() {
	s := state{vel: vec{1, 2, 3, 4, 5, 6, 7, 8}}
	arr := [8]state{}
	for i := 0; i < iters; i++ {
		s = step(s)
		arr[i&7] = s
	}
	var sum int64
	for i := range arr {
		sum += arr[i].pos.a + arr[i].tag[1]
	}
	println("structcopy", sum, s.id)
}
