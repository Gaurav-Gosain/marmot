package specialize

import "testing"

const splitProgram = `package main

import (
	"fmt"
	"strings"
)

func main() {
	inputs := []string{"", ";", ";;", "a", "a;", ";a", "a;b;c", "a;;b", "日;本;語", "x;y;z;", "a\xff;b", "no sep here"}
	for _, s := range inputs {
		n := 0
		for _, f := range strings.Split(s, ";") {
			n++
			fmt.Printf("[%q]", f)
		}
		for i, f := range strings.Split(s, ";") {
			fmt.Printf("<%d:%q>", i, f)
		}
		for i := range strings.Split(s, ";") {
			fmt.Printf("{%d}", i)
		}
		for range strings.Split(s, ";;") {
			fmt.Print(".")
		}
		for _, f := range strings.Split(s, "本") {
			fmt.Printf("(%q)", f)
		}
		fmt.Println(n)
	}
	var fs []func() string
	s := "p;q;r;s"
	for _, f := range strings.Split(s, ";") {
		s = "changed"
		fs = append(fs, func() string { return f })
		if f == "q" {
			continue
		}
		if f == "r" {
			break
		}
		fmt.Println("body", f, s)
	}
	for _, g := range fs {
		fmt.Print(g(), " ")
	}
	fmt.Println()
outer:
	for _, line := range strings.Split("a=1;b=2\nc=3;d\n\ne=5", "\n") {
		for _, kv := range strings.Split(line, ";") {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue outer
			}
			fmt.Println(k, v)
		}
	}
	for _, w := range strings.Fields("  a b\tc\n d e f  ") {
		fmt.Printf("%q ", w)
	}
	fmt.Println()
	var ws []func() string
	for _, w := range strings.Fields("x y z") {
		ws = append(ws, func() string { return w })
	}
	for _, g := range ws {
		fmt.Print(g())
	}
	fmt.Println()
	sep := ";"
	for _, f := range strings.Split("a;b", sep) {
		fmt.Print(f)
	}
	fmt.Println()
}
`

func TestSplitDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, splitProgram, 9)
}
