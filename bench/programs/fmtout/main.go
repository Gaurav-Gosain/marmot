// fmtout: fmt heavy output into a hashing writer, so no terminal speed is measured.
package main

import (
	"fmt"
	"hash/fnv"
)

const lines = 4000000

func main() {
	h := fnv.New64a()
	for i := 0; i < lines; i++ {
		fmt.Fprintf(h, "%d: %s %5.2f %x %v %q\n", i, "row", float64(i)/7, i*31, i%2 == 0, "q")
	}
	fmt.Fprintln(h, "done", lines)
	fmt.Println(h.Sum64())
}
