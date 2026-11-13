// cgo: the pointer passing check. Go memory passed to C may hold no
// unpinned Go pointers; pinned ones and pointer-free memory are fine.
package main

/*
#include <stdlib.h>
static void take(void *p) {}
typedef struct { int *q; } holder;
static void takeh(holder *h) {}
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

type plain struct{ a, b int }

type withPtr struct{ p *int }

func main() {
	pl := &plain{1, 2}
	C.take(unsafe.Pointer(pl))
	fmt.Println("pointer-free memory: ok")

	var h C.holder
	C.takeh(&h)
	fmt.Println("nil pointer inside: ok")

	x := new(int)
	w := &withPtr{x}
	var pin runtime.Pinner
	pin.Pin(x)
	C.take(unsafe.Pointer(w))
	pin.Unpin()
	fmt.Println("pinned pointer inside: ok")

	cp := (*C.int)(C.malloc(4))
	h.q = cp
	C.takeh(&h)
	C.free(unsafe.Pointer(cp))
	fmt.Println("C pointer inside: ok")

	C.take(unsafe.Pointer(w))
	fmt.Println("not reached")
}
