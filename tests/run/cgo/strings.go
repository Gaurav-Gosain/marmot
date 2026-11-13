// cgo: C.CString, C.GoString, C.GoStringN, C.GoBytes, C.CBytes, C.malloc.
package main

/*
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
static void upper(char *s) { for (; *s; s++) *s = toupper((unsigned char)*s); }
static const char *lit(void) { return "from C"; }
static char *join(const char *a, const char *b) {
	char *r = malloc(strlen(a) + strlen(b) + 1);
	strcpy(r, a);
	strcat(r, b);
	return r;
}
static int sumbytes(const unsigned char *p, int n) { int s = 0; for (int i = 0; i < n; i++) s += p[i]; return s; }
static const char *words[] = {"zero", "one", "two"};
static const char *word(int i) { return words[i]; }
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	s := C.CString("hello, cgo")
	C.upper(s)
	fmt.Println(C.GoString(s), C.strlen(s))
	fmt.Println(C.GoStringN(s, 5), C.GoString(nil) == "")
	C.free(unsafe.Pointer(s))
	fmt.Println(C.GoString(C.lit()))
	a, b := C.CString("ab"), C.CString("cd")
	j := C.join(a, b)
	fmt.Println(C.GoString(j))
	C.free(unsafe.Pointer(a))
	C.free(unsafe.Pointer(b))
	C.free(unsafe.Pointer(j))
	data := []byte{1, 2, 3, 250}
	p := C.CBytes(data)
	fmt.Println(C.sumbytes((*C.uchar)(p), C.int(len(data))))
	back := C.GoBytes(p, 4)
	fmt.Println(back, len(C.GoBytes(p, 0)))
	C.free(p)
	m := (*[16]byte)(C.malloc(16))
	for i := range m {
		m[i] = byte(i * i)
	}
	fmt.Println(m[15], C.sumbytes((*C.uchar)(unsafe.Pointer(m)), 16))
	C.free(unsafe.Pointer(m))
	for i := 0; i < 3; i++ {
		fmt.Print(C.GoString(C.word(C.int(i))), " ")
	}
	fmt.Println()
	empty := C.CString("")
	fmt.Println(C.strlen(empty), C.GoString(empty) == "")
	C.free(unsafe.Pointer(empty))
}
