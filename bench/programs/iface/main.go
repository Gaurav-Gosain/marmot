// interface method dispatch through a mixed slice.
package main

const iters = 500000000

type shape interface{ area() int }
type sq struct{ s int }
type rect struct{ w, h int }
type tri struct{ b, h int }

func (s sq) area() int    { return s.s * s.s }
func (r *rect) area() int { return r.w * r.h }
func (t tri) area() int   { return t.b * t.h / 2 }

func main() {
	shapes := []shape{sq{3}, &rect{4, 5}, tri{6, 7}, sq{8}, &rect{2, 9}, tri{10, 3}, sq{1}, &rect{7, 7}}
	sum := 0
	for i := 0; i < iters; i++ {
		sum += shapes[i&7].area()
	}
	println("iface", sum)
}
