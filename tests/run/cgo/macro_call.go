// cgo: a #define that expands to a function call (not a constant), and one
// that builds a struct, read as values. cgo reads them through C wrappers
// (_Cmacro_ functions).
package main

/*
#include <stdbool.h>
typedef struct { unsigned short v; bool ansi; } mode;
static mode mode_new(unsigned short v, bool ansi) { mode m = { v, ansi }; return m; }
static int twice(int x) { return 2 * x; }
#define MODE_COLUMNS (mode_new(3, false))
#define MODE_ANSI (mode_new(4, true))
#define TWICE_TEN (twice(10))
*/
import "C"

import "fmt"

var columns = C.MODE_COLUMNS

func main() {
	a := C.MODE_ANSI
	fmt.Println(columns.v, bool(columns.ansi), a.v, bool(a.ansi))
	fmt.Println(C.TWICE_TEN, int(C.TWICE_TEN)+1)
}
