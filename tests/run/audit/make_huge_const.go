// Make with a constant length too large to
// allocate compiles (x/tools turns it into new([n]T)[:]) and in gc panics
// with "makeslice: len out of range", which recover catches and deferred
// calls see. marmot stops with "fatal error: out of memory".
package main

import "fmt"

var n int

func incr() { n++ }

func big() int {
	defer incr()
	s := make([]byte, 1<<62)
	return len(s)
}

func main() {
	defer func() { fmt.Println("recovered:", recover(), "n:", n) }()
	big()
}
