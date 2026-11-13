package p

/*
#include <errno.h>
#include <string.h>
#include <unistd.h>
typedef struct { int x; double y; } pair;
int counter = 1;
static int add(int a, int b) { return a + b; }
static pair mk(int x) { pair p = {x, x * 0.5}; return p; }
static int fails(void) { errno = 2; return -1; }
static signed char neg(signed char c) { return -c; }
static void spin(void) {}
static void nap(void) { usleep(1); }
#cgo noescape spin
#cgo nocallback spin
*/
import "C"

func Use() {
	_ = C.add(1, 2)
	_ = C.mk(1)
	_, _ = C.fails()
	_ = C.neg(1)
	C.spin()
	C.nap()
	_ = C.strlen(nil)
	_ = C.counter
	_ = C.GoString(nil)
}
