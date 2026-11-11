package main

import (
	"fmt"
	"os"
)

//go:noinline
func join(prefix string, b []byte) string {
	s := prefix + string(b) // prefix is empty at run time: the runtime returns string(b) itself
	return s
}

func main() {
	prefix := ""
	if len(os.Args) > 5 {
		prefix = "x"
	}
	b := []byte("abc")
	s := join(prefix, b)
	b[0] = 'z' // must not change s
	fmt.Println(s)
}
