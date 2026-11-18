// Rule V's cached check loops (MARMOT_LOOPS_VERSION=1) must read row lengths
// only where the program reads them. b's row headers end at a page that cannot
// be read, so a read past the last row faults. The inner loop's row bound comes
// from an if inside the outer loop, so no check in the inner loop guards it.
package main

import (
	"os"
	"syscall"
	"unsafe"
)

const n = 64

var keep [][]float64

// rowsAtPageEnd returns r rows of length m whose headers end exactly where an
// unreadable page starts.
func rowsAtPageEnd(r, m int) [][]float64 {
	pg := os.Getpagesize()
	mem, err := syscall.Mmap(-1, 0, 2*pg, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		panic(err)
	}
	if err := syscall.Mprotect(mem[pg:], syscall.PROT_NONE); err != nil {
		panic(err)
	}
	size := r * int(unsafe.Sizeof([]float64(nil)))
	b := unsafe.Slice((*[]float64)(unsafe.Pointer(&mem[pg-size])), r)
	for i := range b {
		row := make([]float64, m)
		for j := range row {
			row[j] = float64((i+j)%5) - 1.5
		}
		keep = append(keep, row)
		b[i] = row
	}
	return b
}

//go:noinline
func sum(b [][]float64, cols int) int64 {
	s := 0.0
	for j := 0; j < cols; j++ {
		if n <= len(b) {
			for k := 0; k < n; k++ {
				s += b[k][j]
			}
		}
	}
	return int64(s)
}

func main() {
	short := rowsAtPageEnd(10, 8)
	println("short rows", sum(short, 8))
	println("zero columns", sum(short, 0))
	full := rowsAtPageEnd(n, 8)
	println("full rows", sum(full, 8))
	defer func() { println("recovered:", recover() != nil) }()
	println("too many columns", sum(full, 9)) // b[0][8] panics
}
