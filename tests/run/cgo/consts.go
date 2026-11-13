// cgo: enums, #define constants, sizeof and typedefs.
package main

/*
#include <limits.h>
#define ANSWER 42
#define RATIO 0.75
#define GREETING "hello"
#define NEG (-5)
enum color { RED, GREEN = 5, BLUE };
typedef enum { SMALL = 1, LARGE = 1000000 } size_kind;
typedef unsigned long long u64;
typedef struct { int a; char b; } rec;
static int colorval(enum color c) { return (int)c * 10; }
static size_kind pick(int big) { return big ? LARGE : SMALL; }
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	fmt.Println(C.ANSWER, C.RATIO, C.GREETING, C.NEG)
	fmt.Println(C.RED, C.GREEN, C.BLUE, C.colorval(C.BLUE))
	fmt.Println(C.pick(0), C.pick(1), C.SMALL+C.LARGE)
	var c C.enum_color = C.GREEN
	fmt.Println(c, unsafe.Sizeof(c))
	var u C.u64 = 1 << 63
	fmt.Println(u, C.sizeof_rec, C.sizeof_int, C.sizeof_u64, C.INT_MAX, C.CHAR_BIT)
}
