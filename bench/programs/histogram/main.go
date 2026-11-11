// histogram: byte histogram with 256 bins.
package main

const (
	n    = 1 << 18
	reps = 72000
)

func main() {
	buf := make([]byte, n)
	x := uint32(1)
	for i := range buf {
		x = x*1664525 + 1013904223
		buf[i] = byte(x >> 24)
	}
	var hist [256]int
	for r := 0; r < reps; r++ {
		buf[r&(n-1)]++
		for _, b := range buf {
			hist[b]++
		}
	}
	sum := 0
	for i, h := range hist {
		sum += i * h
	}
	println("histogram", sum, hist[0], hist[255])
}
