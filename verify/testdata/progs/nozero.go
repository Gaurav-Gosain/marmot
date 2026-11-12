// nozero: every caller of the allocation without zeroing (rt_alloc_nozero: bytealg.MakeNoZero,
// rawstring, convT16/32/64)
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
	// rawstring callers: concatenation, string(bytes), string(runes), string(int), and boxes
	words := []string{"", "a", "bc", strings.Repeat("d", 40), strings.Repeat("e", 3000)}
	for _, x := range words {
		for _, y := range words {
			r = append(r, sum([]byte(x+y)), sum([]byte(x+y+x)), sum([]byte(x+"-"+y+"-"+x+y)))
		}
		r = append(r, sum([]byte(string([]byte(x)))))
	}
	runes := []rune{'a', 0x7f, 0x80, 0x7ff, 0x800, 0xd800, 0xdfff, 0xffff, 0x10000, 0x10ffff, 0x110000, -1, -0x7fffffff}
	r = append(r, sum([]byte(string(runes))))
	for _, v := range []int64{65, 0x7ff, 0xd800, 0x10ffff, 0x110000, -1, 1 << 40, -(1 << 40)} {
		r = append(r, sum([]byte(string(rune(v)))))
		if rune(v) == rune(int32(v)) && int64(int32(v)) == v {
			r = append(r, sum([]byte(string(rune(int32(v))))))
		}
	}
	var boxes []any
	for i := 0; i < 50; i++ {
		boxes = append(boxes, uint16(300+i*1000), uint32(70000+i*99991), uint64(1<<40+i), int64(-1-i), float64(i)+0.5)
	}
	t := 0
	for _, b := range boxes {
		switch v := b.(type) {
		case uint16:
			t = t*31 + int(v)
		case uint32:
			t = t*31 + int(v)
		case uint64:
			t = t*31 + int(v)
		case int64:
			t = t*31 + int(v)
		case float64:
			t = t*31 + int(v*2)
		}
	}
	r = append(r, t)
	fmt.Println(r)
}
