package main

import "strings"

func count(b []byte, c byte) int {
	n := 0
	for _, x := range b {
		if x == c {
			n++
		}
	}
	return n
}

func firstIndex(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func upper(b []byte) {
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
}

func main() {
	for _, n := range []int{0, 1, 15, 16, 17, 31, 32, 33, 100, 1000} {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte('a' + i%7)
		}
		println(n, count(b, 'c'), firstIndex(b, 'g'), firstIndex(b, 'z'))
		upper(b)
		println(string(b) == strings.ToUpper(string(b)), len(b))
	}
	s := strings.Repeat("abcdefghij", 50) + "X" + strings.Repeat("k", 20)
	println(strings.IndexByte(s, 'X'), strings.Count(s, "k"), strings.Index(s, "jX"), strings.LastIndexByte(s, 'a'))
}
