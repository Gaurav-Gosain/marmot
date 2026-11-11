// conv: 1D convolution with 16 integer taps.
package main

const (
	n    = 1 << 16
	taps = 16
	reps = 32000
)

func main() {
	in := make([]int32, n+taps)
	out := make([]int32, n)
	var k [taps]int32
	for i := range in {
		in[i] = int32(i%23) - 11
	}
	for i := range k {
		k[i] = int32(i%5) - 2
	}
	var total int64
	for r := 0; r < reps; r++ {
		in[r&(n-1)]++
		for i := 0; i < n; i++ {
			var s int32
			for j := 0; j < taps; j++ {
				s += in[i+j] * k[j]
			}
			out[i] = s
		}
		total += int64(out[r&(n-1)])
	}
	println("conv", total)
}
