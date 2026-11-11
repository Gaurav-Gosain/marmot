package main

import (
	"fmt"
	"os"
)

func main() {
	os.Stdout.Write([]byte("out line 1\n"))
	os.Stdout.WriteString("out line 2\n")
	os.Stderr.WriteString("err line 1\n")
	fmt.Fprintln(os.Stdout, "via fmt out")
	fmt.Fprintln(os.Stderr, "via fmt err")
	n, err := os.Stdout.Write(nil)
	println(n, err == nil)
	println(os.Stdin != nil, os.Stdout.Name(), os.Stderr.Name())
	println(os.Getpid() > 0, os.Getpagesize() > 0)
}
