// bytecount: count occurrences of one byte value in a buffer, written as a plain loop.
package main

const (
	n    = 1 << 20
	reps = 32000
)

func count(b []byte, c byte) int {
	n := 0
	for _, x := range b {
		if x == c {
			n++
		}
	}
	return n
}

func main() {
	buf := make([]byte, n)
	x := uint32(3)
	for i := range buf {
		x = x*1664525 + 1013904223
		buf[i] = byte(x>>24) & 15
	}
	total := 0
	for r := 0; r < reps; r++ {
		total += count(buf, byte(r&15))
		buf[r] ^= 1
	}
	println("bytecount", total)
}
