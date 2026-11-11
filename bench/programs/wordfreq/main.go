// wordfreq: build a text in memory, read it with bufio.Scanner, count words in a map.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

const lines = 1200000

func main() {
	vocab := make([]string, 5000)
	x := uint32(1)
	for i := range vocab {
		var sb strings.Builder
		x = x*1664525 + 1013904223
		l := 3 + int(x>>28)%6
		for j := 0; j < l; j++ {
			x = x*1664525 + 1013904223
			sb.WriteByte(byte('a' + (x>>24)%26))
		}
		vocab[i] = sb.String()
	}
	var buf bytes.Buffer
	for i := 0; i < lines; i++ {
		for w := 0; w < 10; w++ {
			x = x*1664525 + 1013904223
			// Skewed pick: small indexes are more common.
			k := int(x>>16) % len(vocab)
			k = k * k / len(vocab)
			buf.WriteString(vocab[k])
			buf.WriteByte(' ')
		}
		buf.WriteByte('\n')
	}
	counts := map[string]int{}
	sc := bufio.NewScanner(&buf)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		for _, w := range strings.Fields(sc.Text()) {
			counts[strings.ToUpper(w)]++
		}
	}
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range counts {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return all[i].k < all[j].k
	})
	fmt.Println("distinct", len(all))
	for _, e := range all[:5] {
		fmt.Println(e.k, e.v)
	}
}
