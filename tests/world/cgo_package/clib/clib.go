// Package clib wraps a small C library that lives next to it: .c and .S
// files, a header in a subdirectory, and #cgo flags.
package clib

/*
#cgo CFLAGS: -I${SRCDIR}/include -DSCALE=3
#include <stdlib.h>
#include "clib.h"
*/
import "C"

import "unsafe"

// Stats accumulates values in a C struct.
type Stats struct{ s C.stats }

func (s *Stats) Add(v float64) { C.stats_add(&s.s, C.double(v)) }

func (s *Stats) Mean() float64 { return float64(C.stats_mean(&s.s)) }

func (s *Stats) Count() int { return int(s.s.count) }

// Reverse reverses a string in C.
func Reverse(s string) string {
	c := C.CString(s)
	defer C.free(unsafe.Pointer(c))
	n := C.clib_reverse(c)
	return C.GoStringN(c, C.int(n))
}

func Scaled(v int) int { return int(C.clib_scaled(C.long(v))) }

func AsmAdd(a, b int) int { return int(C.clib_asm_add(C.long(a), C.long(b))) }

//export goTwice
func goTwice(v C.long) C.long { return 2 * v }

// TwicePlusOne goes Go -> C (clib.c) -> Go (goTwice) -> C.
func TwicePlusOne(v int) int { return int(C.clib_twice_plus_one(C.long(v))) }
