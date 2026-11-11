// stringsearch: strings.Index, Contains, Count and Replace over a large text.
package main

import (
	"fmt"
	"strings"
)

const reps = 60

func main() {
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta", "iota", "kappa"}
	var sb strings.Builder
	x := uint32(5)
	for sb.Len() < 4<<20 {
		x = x*1664525 + 1013904223
		sb.WriteString(words[(x>>20)%10])
		sb.WriteByte(' ')
	}
	text := sb.String()
	total := 0
	for r := 0; r < reps; r++ {
		pos := 0
		for {
			i := strings.Index(text[pos:], "kappa alpha")
			if i < 0 {
				break
			}
			total++
			pos += i + 1
		}
		total += strings.Count(text, "eta")
		if strings.Contains(text, "zzz") {
			total--
		}
		total += len(strings.Replace(text, "iota", "IOTA", -1)) & 0xff
		total += strings.LastIndex(text, "delta") & 0xff
	}
	fmt.Println(total)
}
