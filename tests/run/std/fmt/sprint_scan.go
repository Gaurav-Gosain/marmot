package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func main() {
	s := fmt.Sprintf("%03d|%v|%s", 7, []string{"a", "b"}, errors.New("e"))
	println(s)
	println(fmt.Sprint("a", 1, 2, "b", "c", 3.5), fmt.Sprintln("x", 1) == "x 1\n")
	var a, b int
	var w string
	n, err := fmt.Sscanf("10 20 word", "%d %d %s", &a, &b, &w)
	println(n, err == nil, a, b, w)
	n, err = fmt.Sscan("5 6", &a, &b)
	println(n, a, b)
	fmt.Fprintf(os.Stdout, "to %s\n", "stdout")
	var sb strings.Builder
	fmt.Fprint(&sb, "built ", 1)
	println(sb.String())
	e := fmt.Errorf("wrapped: %w", os.ErrNotExist)
	println(e.Error(), errors.Is(e, os.ErrNotExist))
	fmt.Println(fmt.Sprintf("%v", [][]int{{1}, {2, 3}}), fmt.Sprintf("%8.3s|", "abcdef"))
	var ip *int
	fmt.Println(ip == nil, fmt.Sprint(ip))
	fmt.Printf("%v %v\n", int8(-5), uint8(200))
	fmt.Printf("%6.2f%%\n", 45.678)
}
