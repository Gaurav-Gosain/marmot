// summinmax: sum, min and max of an int32 slice, repeated.
package main

const (
	n    = 1 << 16
	reps = 180000
)

func stats(a []int32) (int64, int32, int32) {
	var s int64
	lo, hi := a[0], a[0]
	for _, v := range a {
		s += int64(v)
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return s, lo, hi
}

func main() {
	a := make([]int32, n)
	x := uint32(7)
	for i := range a {
		x = x*1664525 + 1013904223
		a[i] = int32(x>>8) - 1<<23
	}
	var ts int64
	var tl, th int64
	for r := 0; r < reps; r++ {
		a[r&(n-1)] += 3
		s, lo, hi := stats(a)
		ts += s
		tl += int64(lo)
		th += int64(hi)
	}
	println("summinmax", ts, tl, th)
}
