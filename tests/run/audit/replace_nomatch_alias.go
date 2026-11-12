// strings.Replace and ReplaceAll return s itself when nothing matches (no
// copy, no allocation). marmot's SIMD path for len(s) >= 256 returns a copy.
package main

import (
	"fmt"
	"strings"
	"unsafe"
)

func main() {
	for _, n := range []int{10, 255, 256, 1000} {
		s := strings.Repeat("ab", n)[:n]
		out := strings.Replace(s, "q", "r", -1)
		all := strings.ReplaceAll(s, "zz", "")
		fmt.Println(n, out == s, unsafe.StringData(out) == unsafe.StringData(s), unsafe.StringData(all) == unsafe.StringData(s))
	}
}
