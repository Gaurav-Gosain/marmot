package main

import "strings"

func show(p []string) {
	print(len(p), ":")
	for _, s := range p {
		print("[", s, "]")
	}
	println()
}

func main() {
	show(strings.Split("a,b,,c", ","))
	show(strings.Split("", ","))
	show(strings.Split("abc", ""))
	show(strings.SplitN("a,b,c,d", ",", 3))
	show(strings.SplitN("a,b", ",", 0))
	show(strings.SplitN("a,b,c", ",", -1))
	show(strings.SplitAfter("a,b,c", ","))
	show(strings.SplitAfterN("a,b,c", ",", 2))
	show(strings.Fields("  a \t b\n\nc  "))
	show(strings.Fields(""))
	show(strings.FieldsFunc("a1b22c", func(r rune) bool { return r >= '0' && r <= '9' }))
}
