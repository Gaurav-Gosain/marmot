// purego v0.10.x, the version tuios gets through gopsutil, with gopsutil's
// way of registering C functions: a generic dlFunc[T] whose init passes
// &d.fn (a T, a type parameter) to RegisterLibFunc, cached per symbol in a
// map. Also a direct RegisterLibFunc and a Go callback through libc's
// qsort (v0.10 keeps its callbacks in syscall_sysv.go).
package main

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type library struct {
	handle uintptr
	mu     sync.RWMutex
	fnMap  map[string]any
}

type dlFunc[T any] struct {
	sym string
	fn  T
}

func (d *dlFunc[T]) init(handle uintptr) {
	purego.RegisterLibFunc(&d.fn, handle, d.sym)
}

// getFunc resolves a C function once and caches it, as gopsutil does.
func getFunc[T any](lib *library, symbol string) T {
	lib.mu.RLock()
	if f, ok := lib.fnMap[symbol].(*dlFunc[T]); ok {
		lib.mu.RUnlock()
		return f.fn
	}
	lib.mu.RUnlock()
	lib.mu.Lock()
	defer lib.mu.Unlock()
	d := &dlFunc[T]{sym: symbol}
	d.init(lib.handle)
	lib.fnMap[symbol] = d
	return d.fn
}

type (
	StrlenFunc  func(s string) int
	AbsFunc     func(x int32) int32
	ToupperFunc func(c int32) int32
)

func libc() string {
	if runtime.GOOS == "darwin" {
		return "/usr/lib/libSystem.B.dylib"
	}
	return "libc.so.6"
}

func main() {
	h, err := purego.Dlopen(libc(), purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	lib := &library{handle: h, fnMap: map[string]any{}}

	strlen := getFunc[StrlenFunc](lib, "strlen")
	fmt.Println("strlen:", strlen("marmot"), getFunc[StrlenFunc](lib, "strlen")("hi"))
	fmt.Println("abs:", getFunc[AbsFunc](lib, "abs")(-42))
	var up []byte
	for _, c := range "purego" {
		up = append(up, byte(getFunc[ToupperFunc](lib, "toupper")(c)))
	}
	fmt.Println("toupper:", string(up))

	var labs func(int64) int64
	purego.RegisterLibFunc(&labs, h, "labs")
	fmt.Println("labs:", labs(-7))

	var qsort func(base unsafe.Pointer, n, size uintptr, cmp uintptr)
	purego.RegisterLibFunc(&qsort, h, "qsort")
	calls := 0
	cmp := purego.NewCallback(func(a, b unsafe.Pointer) int {
		calls++
		x, y := *(*int32)(a), *(*int32)(b)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
	xs := []int32{5, -3, 9, 0, 2, 7, -8}
	qsort(unsafe.Pointer(&xs[0]), uintptr(len(xs)), 4, cmp)
	fmt.Println("qsort:", xs, calls > 0)
}
