// fmtbuf: fmt formatting into memory only (Sprintf, Appendf, Fprintf to a
// bytes.Buffer), so no hashing or I/O is measured.
package main

import (
	"bytes"
	"fmt"
)

const lines = 2000000

type point struct {
	X, Y int
	Tag  string
}

func main() {
	var buf bytes.Buffer
	var app []byte
	total := 0
	for i := 0; i < lines; i++ {
		s := fmt.Sprintf("id=%d name=%s v=%.2f ok=%t", i, "item", float64(i)/3, i%2 == 0)
		total += len(s)
		app = fmt.Appendf(app[:0], "%08x|%-6s|%+d", i, "ab", -i)
		total += len(app)
		buf.Reset()
		fmt.Fprintf(&buf, "%v %q %5.1f", point{i, -i, "t"}, "q", 2.5)
		total += buf.Len()
	}
	fmt.Println(total)
}
