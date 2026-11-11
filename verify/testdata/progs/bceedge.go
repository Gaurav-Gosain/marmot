// bceedge: go-bce B3 side conditions at their edges. Every loop here must keep its bounds
// check (or fail exactly where Go fails); output must match stock Go.
package main

import "fmt"

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "panic:", r)
		}
	}()
	f()
	fmt.Println(name, "ok")
}

//go:noinline
func le(s []int) (t int) { // guard i <= len: B3 must not apply (last index out of range)
	for i := 0; i <= len(s); i++ {
		t += s[i]
	}
	return
}

//go:noinline
func negStart(s []int) (t int) { // start -1: B3 needs a non-negative start constant
	for i := -1; i < len(s); i++ {
		t += s[i]
	}
	return
}

//go:noinline
func step2(s []int) (t int) { // i += 2 against len-1 bound: in range, checks may stay
	for i := 0; i < len(s); i += 2 {
		t += s[i]
	}
	return
}

//go:noinline
func otherLen(s, u []int) (t int) { // guard on len(u), index into s: lengths differ
	for i := 0; i < len(u); i++ {
		t += s[i]
	}
	return
}

//go:noinline
func reslice(s []int) (t int) { // the slice changes inside the loop: n is not the guard's m
	for i := 0; i < len(s); i++ {
		s = s[:len(s)-1]
		if i < len(s) {
			t += s[i]
		}
	}
	return
}

//go:noinline
func narrow(s []int) (t int) { // int8 counter wraps at 127 before reaching len 200
	for i := int8(0); int(i) < len(s); i++ {
		t += s[i]
		if t > 1<<40 {
			break
		}
	}
	return
}

func main() {
	s := make([]int, 200)
	for i := range s {
		s[i] = i
	}
	try("le", func() { fmt.Println(le(s)) })
	try("negStart", func() { fmt.Println(negStart(s)) })
	try("step2", func() { fmt.Println(step2(s)) })
	try("otherLen", func() { fmt.Println(otherLen(s[:10], s)) })
	try("reslice", func() { fmt.Println(reslice(s)) })
	try("narrow", func() { fmt.Println(narrow(s)) })
}
