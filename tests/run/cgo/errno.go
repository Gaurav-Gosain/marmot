// cgo: errno as a second result.
package main

/*
#include <errno.h>
#include <unistd.h>
static int fail(int e) { errno = e; return -1; }
static int ok(void) { return 7; }
static void voidfail(void) { errno = EACCES; }
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func main() {
	r, err := C.fail(C.ENOENT)
	fmt.Println(r, err, errors.Is(err, os.ErrNotExist))
	r, err = C.ok()
	fmt.Println(r, err)
	_, err = C.voidfail()
	fmt.Println(err, err == syscall.EACCES)
	_, err = C.close(-1)
	fmt.Println(err)
	for i := 0; i < 100; i++ {
		if _, err := C.fail(C.int(i%5 + 1)); err != syscall.Errno(i%5+1) {
			fmt.Println("wrong errno", i, err)
		}
	}
	fmt.Println("done")
}
