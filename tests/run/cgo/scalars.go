// cgo: integer, floating point, bool and pointer arguments and results.
package main

/*
#include <stdint.h>
static signed char neg(signed char x) { return -x; }
static unsigned char inc8(unsigned char x) { return x + 1; }
static short sub16(short a, short b) { return a - b; }
static unsigned short mul16(unsigned short a, unsigned short b) { return a * b; }
static int add(int a, int b) { return a + b; }
static unsigned int shr(unsigned int a, int n) { return a >> n; }
static long long big(long long a) { return a * 1000000007LL; }
static uint64_t mix(uint64_t a, uint32_t b, int8_t c, int16_t d) { return a ^ ((uint64_t)b << 8) ^ (uint64_t)(int64_t)c ^ (uint64_t)(int64_t)d; }
static float half(float x) { return x / 2; }
static double hyp(double a, double b) { return a * a + b * b; }
static double many(int a, double b, int c, double d, int e, double f, int g, double h, int i, double j) {
	return a + b + c + d + e + f + g + h + i + j;
}
static long sum9(long a, long b, long c, long d, long e, long f, long g, long h, long i) { return a+b+c+d+e+f+g+h+i; }
static _Bool odd(int x) { return x & 1; }
static void bump(int *p) { (*p)++; }
static int *same(int *p) { return p; }
static char sign(void) { return -3; }
*/
import "C"

import "fmt"

func main() {
	fmt.Println(C.neg(5), C.inc8(255), C.sub16(-3, 4), C.mul16(300, 300))
	fmt.Println(C.add(1, 2), C.add(-7, 3), C.shr(0x80000000, 31))
	fmt.Println(C.big(123456789), C.mix(1<<40, 0xfffffff0, -1, -2))
	fmt.Println(C.half(3), C.hyp(3, 4))
	fmt.Println(C.many(1, 0.5, 2, 0.25, 3, 0.125, 4, 0.0625, 5, 0.03125))
	fmt.Println(C.sum9(1, 2, 3, 4, 5, 6, 7, 8, 9))
	fmt.Println(C.odd(3), C.odd(4), bool(C.odd(5)))
	x := C.int(41)
	C.bump(&x)
	fmt.Println(x, *C.same(&x), C.same(nil) == nil)
	fmt.Println(C.sign(), int(C.sign())+10)
	var total C.int
	for i := C.int(0); i < 1000; i++ {
		total = C.add(total, i)
	}
	fmt.Println(total)
}
