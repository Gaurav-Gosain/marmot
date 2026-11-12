// Edge cases for backend optimizations; the expected output is from gc.
package main

import "fmt"

func try(name string, f func() int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "panic:", r)
		}
	}()
	fmt.Println(name, f())
}

//go:noinline
func appendInLoop(s []int) int {
	t := 0
	for i := 0; i < len(s); i++ {
		t += s[i]
		if i == 1 {
			s = s[:1] // shrink: the next s[i] must be checked against the new len
		}
	}
	return t
}

//go:noinline
func shadowLen(s []int, n int) int {
	t := 0
	for i := 0; i < n; i++ { // n is not len(s)
		t += s[i]
	}
	return t
}

//go:noinline
func int8Loop(s []int) int {
	t := 0
	for i := int8(100); i < 127; i++ {
		t += s[int(i)-100]
		if i == 126 {
			break
		}
	}
	return t
}

//go:noinline
func wrapLoop(s []byte) int {
	t := 0
	var i uint8
	for i = 250; i != 4; i++ { // wraps through 255 to 0
		t += int(s[i])
	}
	return t
}

//go:noinline
func negStart(s []int, k int) int {
	t := 0
	for i := k; i < len(s); i++ {
		t += s[i]
	}
	return t
}

//go:noinline
func twoSlices(a, b []int) int {
	t := 0
	for i := 0; i < len(a); i++ {
		t += b[i] // checked against len(b)
	}
	return t
}

//go:noinline
func aliasHeader(p *[]int) int {
	t := 0
	s := *p
	for i := 0; i < len(s); i++ {
		t += (*p)[i]
		*p = (*p)[:0]
	}
	return t
}

//go:noinline
func stepTwo(s []int) int {
	t := 0
	for i := 0; i < len(s); i += 2 {
		t += s[i+1]
	}
	return t
}

//go:noinline
func reslice(s []int) int {
	t := 0
	for i := 0; i < len(s); i++ {
		s = s[1:]
		t += s[i]
	}
	return t
}

//go:noinline
func dominated(s []int, i int) int {
	if i&1 == 0 {
		_ = s[i]
	}
	return s[i]
}

//go:noinline
func nilPtr(p *[4]int, q *[4]int) int {
	x := 0
	if q != nil {
		x = q[0]
	}
	return x + p[1]
}

//go:noinline
func strIdx(s string, n int) byte {
	var b byte
	for i := 0; i < n; i++ {
		b ^= s[i]
	}
	return b
}

func main() {
	try("appendInLoop", func() int { return appendInLoop([]int{1, 2, 3, 4}) })
	try("shadowLen", func() int { return shadowLen([]int{1, 2, 3}, 4) })
	try("int8Loop", func() int { return int8Loop(make([]int, 20)) })
	try("wrapLoop", func() int { return wrapLoop(make([]byte, 251)) })
	try("negStart", func() int { return negStart([]int{1, 2, 3}, -1) })
	try("twoSlices", func() int { return twoSlices([]int{1, 2, 3}, []int{1, 2}) })
	try("aliasHeader", func() int { s := []int{5, 6, 7}; return aliasHeader(&s) })
	try("stepTwo", func() int { return stepTwo([]int{1, 2, 3}) })
	try("reslice", func() int { return reslice([]int{1, 2, 3, 4}) })
	try("dominated", func() int { return dominated([]int{1, 2}, 3) })
	try("dominatedOdd", func() int { return dominated([]int{1, 2}, 2) })
	try("nilPtr", func() int { return nilPtr(nil, &[4]int{1}) })
	try("strIdx", func() int { return int(strIdx("abc", 4)) })
}
