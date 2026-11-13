// cgo: structs and unions by value and by pointer.
package main

/*
#include <stdlib.h>
#include <string.h>
typedef struct { int x; double y; } pair;
typedef struct { float a, b; } hfa;
typedef struct { long a, b, c, d; } quad;
typedef struct { char tag; short s; long long v; } mixed;
typedef struct { pair p; int arr[3]; } nested;
typedef union { int i; float f; unsigned char b[4]; } un;
struct named { int id; const char *name; };

static pair mkpair(int x) { pair p = {x, x * 1.5}; return p; }
static double pairsum(pair p) { return p.x + p.y; }
static hfa swap(hfa h) { hfa r = {h.b, h.a}; return r; }
static quad scale(quad q, long k) { quad r = {q.a * k, q.b * k, q.c * k, q.d * k}; return r; }
static long quadsum(quad q) { return q.a + q.b + q.c + q.d; }
static mixed mkmixed(char t) { mixed m = {t, -2, 1LL << 40}; return m; }
static nested mknested(void) { nested n = {{7, 0.5}, {1, 2, 3}}; return n; }
static int nestedsum(nested n) { return n.p.x + n.arr[0] + n.arr[1] + n.arr[2]; }
static un mkun(int i) { un u; u.i = i; return u; }
static void fill(pair *p) { p->x = 99; p->y = -1; }
static int namelen(struct named *n) { return (int)strlen(n->name) + n->id; }
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	p := C.mkpair(4)
	fmt.Println(p.x, float64(p.y), float64(C.pairsum(p)))
	h := C.swap(C.hfa{1.5, 2.5})
	fmt.Println(float32(h.a), float32(h.b))
	q := C.scale(C.quad{1, 2, 3, 4}, 10)
	fmt.Println(q.a, q.b, q.c, q.d, C.quadsum(q))
	m := C.mkmixed('z')
	fmt.Println(m.tag, m.s, m.v)
	n := C.mknested()
	fmt.Println(n.p.x, float64(n.p.y), n.arr, C.nestedsum(n))
	u := C.mkun(0x01020304)
	fmt.Println(len(u), u[0], u[3])
	var f C.pair
	C.fill(&f)
	fmt.Println(f.x, float64(f.y))
	name := C.CString("marmot")
	defer C.free(unsafe.Pointer(name))
	nm := C.struct_named{id: 3, name: name}
	fmt.Println(C.namelen(&nm), unsafe.Sizeof(nm), unsafe.Sizeof(p), unsafe.Sizeof(m))
}
