// Huffman coding: frequency analysis, tree construction with a heap, canonical codes,
// bit-level encoding and decoding, and a compression ratio report.
package main

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"
)

type node struct {
	sym         int // byte value, or -1 for internal nodes
	freq        int
	left, right *node
	order       int
}

type nodeHeap []*node

func (h nodeHeap) Len() int { return len(h) }
func (h nodeHeap) Less(i, j int) bool {
	if h[i].freq != h[j].freq {
		return h[i].freq < h[j].freq
	}
	return h[i].order < h[j].order
}
func (h nodeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *nodeHeap) Push(x any)   { *h = append(*h, x.(*node)) }
func (h *nodeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func build(freq [256]int) *node {
	h := &nodeHeap{}
	order := 0
	for s, f := range freq {
		if f > 0 {
			heap.Push(h, &node{sym: s, freq: f, order: order})
			order++
		}
	}
	if h.Len() == 0 {
		return nil
	}
	if h.Len() == 1 {
		only := heap.Pop(h).(*node)
		return &node{sym: -1, freq: only.freq, left: only, order: order}
	}
	for h.Len() > 1 {
		a := heap.Pop(h).(*node)
		b := heap.Pop(h).(*node)
		heap.Push(h, &node{sym: -1, freq: a.freq + b.freq, left: a, right: b, order: order})
		order++
	}
	return heap.Pop(h).(*node)
}

type Code struct {
	Bits uint32
	Len  int
}

func lengths(n *node, depth int, out map[int]int) {
	if n == nil {
		return
	}
	if n.sym >= 0 {
		out[n.sym] = max(depth, 1)
		return
	}
	lengths(n.left, depth+1, out)
	lengths(n.right, depth+1, out)
}

// canonical assigns codes by (length, symbol) order so the table can be rebuilt from lengths.
func canonical(lens map[int]int) map[int]Code {
	type sl struct{ sym, l int }
	var list []sl
	for s, l := range lens {
		list = append(list, sl{s, l})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].l != list[j].l {
			return list[i].l < list[j].l
		}
		return list[i].sym < list[j].sym
	})
	codes := map[int]Code{}
	var code uint32
	prev := 0
	for i, e := range list {
		if i > 0 {
			code = (code + 1) << uint(e.l-prev)
		}
		prev = e.l
		codes[e.sym] = Code{code, e.l}
	}
	return codes
}

type BitWriter struct {
	buf  []byte
	cur  byte
	nbit int
	n    int
}

func (w *BitWriter) Write(c Code) {
	for i := c.Len - 1; i >= 0; i-- {
		w.cur = w.cur<<1 | byte(c.Bits>>uint(i)&1)
		w.nbit++
		w.n++
		if w.nbit == 8 {
			w.buf = append(w.buf, w.cur)
			w.cur, w.nbit = 0, 0
		}
	}
}

func (w *BitWriter) Bytes() []byte {
	if w.nbit > 0 {
		return append(append([]byte(nil), w.buf...), w.cur<<uint(8-w.nbit))
	}
	return w.buf
}

func encode(data []byte, codes map[int]Code) ([]byte, int) {
	var w BitWriter
	for _, b := range data {
		w.Write(codes[int(b)])
	}
	return w.Bytes(), w.n
}

func decode(enc []byte, nbits int, codes map[int]Code) ([]byte, error) {
	type key struct {
		bits uint32
		l    int
	}
	rev := map[key]byte{}
	maxLen := 0
	for s, c := range codes {
		rev[key{c.Bits, c.Len}] = byte(s)
		maxLen = max(maxLen, c.Len)
	}
	var out []byte
	var cur uint32
	l := 0
	for i := 0; i < nbits; i++ {
		bit := enc[i/8] >> uint(7-i%8) & 1
		cur = cur<<1 | uint32(bit)
		l++
		if s, ok := rev[key{cur, l}]; ok {
			out = append(out, s)
			cur, l = 0, 0
		} else if l > maxLen {
			return out, fmt.Errorf("invalid code at bit %d", i)
		}
	}
	if l != 0 {
		return out, fmt.Errorf("trailing bits")
	}
	return out, nil
}

func analyze(name, text string) {
	var freq [256]int
	for i := 0; i < len(text); i++ {
		freq[text[i]]++
	}
	tree := build(freq)
	if tree == nil {
		fmt.Printf("%s: empty input\n", name)
		return
	}
	lens := map[int]int{}
	lengths(tree, 0, lens)
	codes := canonical(lens)
	enc, nbits := encode([]byte(text), codes)
	dec, err := decode(enc, nbits, codes)
	ratio := float64(nbits) / float64(8*len(text))
	var avg float64
	for s, c := range codes {
		avg += float64(freq[s]) * float64(c.Len)
	}
	avg /= float64(len(text))
	fmt.Printf("%s: %d bytes, %d symbols, %d bits (%d bytes), ratio %.3f, avg code length %.3f, roundtrip=%v err=%v\n",
		name, len(text), len(codes), nbits, len(enc), ratio, avg, string(dec) == text, err)
	if len(codes) <= 12 {
		var syms []int
		for s := range codes {
			syms = append(syms, s)
		}
		sort.Slice(syms, func(i, j int) bool {
			if codes[syms[i]].Len != codes[syms[j]].Len {
				return codes[syms[i]].Len < codes[syms[j]].Len
			}
			return syms[i] < syms[j]
		})
		for _, s := range syms {
			c := codes[s]
			fmt.Printf("  %q freq=%d code=%0*b\n", rune(s), freq[s], c.Len, c.Bits)
		}
	}
	// prefix-free check
	var list []string
	for _, c := range codes {
		list = append(list, fmt.Sprintf("%0*b", c.Len, c.Bits))
	}
	sort.Strings(list)
	ok := true
	for i := 1; i < len(list); i++ {
		if strings.HasPrefix(list[i], list[i-1]) {
			ok = false
		}
	}
	fmt.Println("  prefix free:", ok)
}

func main() {
	analyze("abracadabra", "abracadabra")
	analyze("single", "aaaaaaaa")
	analyze("empty", "")
	analyze("two", "abababababba")
	analyze("pangram", "the quick brown fox jumps over the lazy dog")
	var sb strings.Builder
	x := uint32(7)
	for i := 0; i < 20000; i++ {
		x = x*1664525 + 1013904223
		r := x >> 24
		switch {
		case r < 128:
			sb.WriteByte('e')
		case r < 192:
			sb.WriteByte('t')
		case r < 224:
			sb.WriteByte('a')
		case r < 240:
			sb.WriteByte('o')
		default:
			sb.WriteByte(byte('a' + r%26))
		}
	}
	analyze("skewed", sb.String())
	var all strings.Builder
	for i := 0; i < 256; i++ {
		all.WriteString(strings.Repeat(string([]byte{byte(i)}), i%5+1))
	}
	analyze("all bytes", all.String())
	_, err := decode([]byte{0xff}, 8, map[int]Code{'a': {0, 1}, 'b': {2, 2}})
	fmt.Println(err)
}
