// cgo: C function pointers passed around and called by C.
package main

/*
typedef int (*binop)(int, int);
static int mul(int a, int b) { return a * b; }
static int sub(int a, int b) { return a - b; }
static binop pick(int which) { return which ? mul : sub; }
static int apply(binop f, int a, int b) { return f(a, b); }
static int fold(binop f, int *xs, int n, int init) { for (int i = 0; i < n; i++) init = f(init, xs[i]); return init; }
*/
import "C"

import "fmt"

func main() {
	m, s := C.pick(1), C.pick(0)
	fmt.Println(C.apply(m, 6, 7), C.apply(s, 6, 7), m != s)
	xs := []C.int{1, 2, 3, 4, 5}
	fmt.Println(C.fold(m, &xs[0], C.int(len(xs)), 1), C.fold(s, &xs[0], 5, 100))
	var f C.binop = m
	fmt.Println(C.apply(f, 3, 3), f == m)
}
