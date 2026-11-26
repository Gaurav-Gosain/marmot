// cgo: an //export function whose result is C.bool, called from C.
package main

/*
#include <stdbool.h>
extern bool goEven(int);
static int countEven(int n) { int c = 0; for (int i = 0; i < n; i++) if (goEven(i)) c++; return c; }
static bool firstEven(void) { return goEven(0) && !goEven(1); }
*/
import "C"

import "fmt"

//export goEven
func goEven(i C.int) C.bool { return i%2 == 0 }

func main() {
	fmt.Println(C.countEven(7), bool(C.firstEven()))
}
