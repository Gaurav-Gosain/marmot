// nozero: every caller of bytealg.MakeNoZero (the allocation without zeroing, rt_alloc_nozero)
// must overwrite all bytes it exposes, including what reslicing up to cap can reach. Each
// result is checked byte for byte, and cap is printed where the caller limits it. Run with
// MARMOT_POISON=1 (nozero memory filled with a pattern) to make a missed byte visible.
package main

import (
	"bytes"
	"fmt"
	"strings"
)

func sum(b []byte) (n int) {
	for _, c := range b {
		n = n*31 + int(c)
	}
	return n
}

func main() {
	var r []int
	for _, n := range []int{1, 7, 8, 31, 33, 255, 256, 257, 1000, 5000} {
		s := strings.Repeat("aB-", n)[:n]
		r = append(r, sum([]byte(strings.ToUpper(s))), sum([]byte(strings.ToLower(s))))
		long := strings.Repeat("xay", n)
		r = append(r, sum([]byte(strings.ReplaceAll(long, "a", ""))), sum([]byte(strings.ReplaceAll(long, "ay", "Q"))))
		r = append(r, sum([]byte(strings.Replace(long, "a", "", n/2+1))))
		b := []byte(s)
		u, l := bytes.ToUpper(b), bytes.ToLower(b)
		r = append(r, sum(u), cap(u)-len(u), sum(l), cap(l)-len(l))
		j := bytes.Join([][]byte{b, b[:n/2], nil, b}, []byte("--"))
		r = append(r, sum(j), cap(j)-len(j))
		p := bytes.Repeat(b[:min(len(b), 1+n%5)], n)
		r = append(r, sum(p), cap(p)-len(p))
		var sb strings.Builder
		sb.Grow(n)
		sb.WriteString(s[:n/2])
		sb.Grow(3 * n)
		sb.WriteString(s)
		r = append(r, sum([]byte(sb.String())))
	}
	fmt.Println(r)
}
