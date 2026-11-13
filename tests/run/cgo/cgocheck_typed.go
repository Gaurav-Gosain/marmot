// cgo: the pointer passing check on a typed pointer argument.
package main

/*
typedef struct { int *q; } holder;
static void takeh(holder *h) {}
*/
import "C"

import "fmt"

func main() {
	var h C.holder
	C.takeh(&h)
	fmt.Println("nil pointer inside: ok")
	goInt := new(C.int)
	bad := &C.holder{q: goInt}
	C.takeh(bad)
	fmt.Println("not reached")
}
