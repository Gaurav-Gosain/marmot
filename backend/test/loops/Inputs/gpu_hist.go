package main

import "time"

const n = 1 << 26

//go:noinline
func histo(b []byte) [256]int {
	var h [256]int
	for _, x := range b {
		h[x]++
	}
	return h
}

func main() {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	// GPU setup runs on a startup thread; give it time to finish.
	time.Sleep(time.Second)
	var h [256]int
	for r := 0; r < 4; r++ {
		h = histo(b)
	}
	println("hist", h[0], h[17], h[255])
}
