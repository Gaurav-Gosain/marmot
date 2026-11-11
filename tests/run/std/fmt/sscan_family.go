package main

import "fmt"

func main() {
	var a, b int
	var s string
	var f float64
	var c rune
	n, err := fmt.Sscanf("10 20 hello 2.5 x", "%d %d %s %f %c", &a, &b, &s, &f, &c)
	println(n, err == nil, a, b, s, f, c)
	n, err = fmt.Sscan("7 8", &a, &b)
	println(n, err == nil, a, b)
	n, err = fmt.Sscanf("age:42", "age:%d", &a)
	println(n, err == nil, a)
	n, err = fmt.Sscanf("abc", "%d", &a)
	println(n, err != nil)
	n, err = fmt.Sscan("only", &s, &a)
	println(n, err != nil, s)
	var x uint8
	n, err = fmt.Sscan("300", &x)
	println(n, err != nil)
	var h int
	fmt.Sscanf("ff", "%x", &h)
	println(h)
	var q string
	fmt.Sscanf(`"quoted str"`, "%q", &q)
	println(q)
	var bl bool
	fmt.Sscan("true", &bl)
	println(bl)
	n, err = fmt.Sscanln("1 2\n3", &a, &b)
	println(n, err == nil)
}
