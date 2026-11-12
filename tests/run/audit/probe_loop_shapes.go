// Audit probe (loops, 2026-11-12): adversarial program for the opt-in loop
// transforms; matched go with -par -type-alias and -loops-force-par in dev,
// -release and GC stress. Products are rounded with float64() so gc on
// arm64 (which fuses a*b+c) and amd64 agree. See docs/audit-backend.md.
package main

import (
	"fmt"
	"unsafe"
)

const n = 1 << 18

func fill(buf []byte, seed uint32) {
	x := seed
	for i := range buf {
		x = x*1664525 + 1013904223
		buf[i] = byte(x >> 24)
	}
}

//go:noinline
func histLocal(buf []byte) (int, int, int) {
	var hist [256]int
	for r := 0; r < 20; r++ {
		buf[r&(n-1)]++
		for _, b := range buf {
			hist[b]++
		}
	}
	s := 0
	for i, h := range hist {
		s += i * h
	}
	return s, hist[0], hist[255]
}

//go:noinline
func histU8(buf []byte) (int, uint8) {
	var hist [256]uint8 // counters wrap
	for r := 0; r < 3; r++ {
		for _, b := range buf {
			hist[b]++
		}
	}
	s := 0
	for i, h := range hist {
		s += i * int(h)
	}
	return s, hist[7]
}

// The bytes being counted are the counters themselves.
//
//go:noinline
func histSelf() (int, int) {
	var hist [256]int
	view := unsafe.Slice((*byte)(unsafe.Pointer(&hist[0])), 256*8)
	for i := range view {
		view[i] = byte(i % 3)
	}
	for r := 0; r < 4; r++ {
		for _, b := range view {
			hist[b]++
		}
	}
	return int(hist[0]), int(hist[2])
}

//go:noinline
func scanInt(a []int, reps int) int {
	t := 0
	for r := 0; r < reps; r++ {
		for i := 1; i < len(a); i++ {
			a[i] += a[i-1]
		}
		t += a[len(a)-1]
		for i := range a {
			a[i] &= 0xffff
		}
	}
	return t
}

//go:noinline
func scanI32(a []int32) int32 {
	for i := 1; i < len(a); i++ {
		a[i] += a[i-1]
	}
	return a[len(a)-1]
}

//go:noinline
func scanF(a []float64) float64 {
	for i := 1; i < len(a); i++ {
		a[i] += a[i-1]
	}
	return a[len(a)-1]
}

// Scan that also reads a second slice overlapping the first.
//
//go:noinline
func scanOverlap(a, b []int) int {
	for i := 1; i < len(a); i++ {
		a[i] += a[i-1] + b[i]
	}
	return a[len(a)-1]
}

const size = 400

//go:noinline
func mandel() int {
	inside := 0
	for py := 0; py < size; py++ {
		ci := float64(2.0*float64(py)/size) - 1.0
		for px := 0; px < size; px++ {
			cr := float64(2.0*float64(px)/size) - 1.5
			zr, zi := 0.0, 0.0
			i := 0
			for ; i < 50 && float64(zr*zr)+float64(zi*zi) <= 4.0; i++ {
				zr, zi = float64(zr*zr)-float64(zi*zi)+cr, float64(2*zr*zi)+ci
			}
			if i == 50 {
				inside++
			}
		}
	}
	return inside
}

//go:noinline
func mandelSum() (int, float64) {
	inside := 0
	acc := 0.0
	for py := 0; py < size; py++ {
		ci := float64(2.0*float64(py)/size) - 1.0
		for px := 0; px < size; px++ {
			cr := float64(2.0*float64(px)/size) - 1.5
			zr, zi := 0.0, 0.0
			i := 0
			for ; i < 50 && float64(zr*zr)+float64(zi*zi) <= 4.0; i++ {
				zr, zi = float64(zr*zr)-float64(zi*zi)+cr, float64(2*zr*zi)+ci
			}
			if i == 50 {
				inside++
			}
			acc += float64(zr * 1e-3) // FP live out of the inner loop, summed in order
		}
	}
	return inside, acc
}

//go:noinline
func mandelRows(rows []int) {
	for py := 0; py < size; py++ {
		ci := float64(2.0*float64(py)/size) - 1.0
		c := 0
		for px := 0; px < size; px++ {
			cr := float64(2.0*float64(px)/size) - 1.5
			zr, zi := 0.0, 0.0
			i := 0
			for ; i < 50 && float64(zr*zr)+float64(zi*zi) <= 4.0; i++ {
				zr, zi = float64(zr*zr)-float64(zi*zi)+cr, float64(2*zr*zi)+ci
			}
			c += i
		}
		rows[py] = c
	}
}

func main() {
	buf := make([]byte, n)
	fill(buf, 1)
	fmt.Println(histLocal(buf))
	fmt.Println(histU8(buf))
	fmt.Println(histSelf())

	a := make([]int, n)
	for i := range a {
		a[i] = i % 97
	}
	fmt.Println(scanInt(a, 10))
	a32 := make([]int32, n)
	for i := range a32 {
		a32[i] = int32(i * 40503)
	}
	fmt.Println(scanI32(a32))
	af := make([]float64, n)
	for i := range af {
		af[i] = 1e16
		if i%3 != 0 {
			af[i] = 0.7
		}
	}
	fmt.Printf("%.17g\n", scanF(af))
	for i := range a {
		a[i] = i % 13
	}
	fmt.Println(scanOverlap(a[:n-8], a[8:]), a[100], a[n-9])
	fmt.Println(mandel())
	ins, acc := mandelSum()
	fmt.Printf("%d %.17g\n", ins, acc)
	rows := make([]int, size)
	mandelRows(rows)
	t := 0
	for _, r := range rows {
		t += r
	}
	fmt.Println(t, rows[size/2])
}
