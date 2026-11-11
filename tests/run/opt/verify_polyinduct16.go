package main

import "fmt"

//go:noinline
func fill(buf []int16) {
	var a, d int16
	for i := 0; i < len(buf); i++ {
		d += 1
		a += d
		buf[i] = a
	}
}

func main() {
	n := 1 << 20
	buf := make([]int16, n)
	fill(buf)
	var h uint64
	for _, v := range buf {
		h = h*31 + uint64(uint16(v))
	}
	fmt.Println(h, buf[65535], buf[65536], buf[n-1])
}
