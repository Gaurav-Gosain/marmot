// Edge cases for backend optimizations; the expected output is from gc.
package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
	_ "unsafe"
)

var order []string

func note(s string) int { order = append(order, s); return len(order) }

// Initialization order by dependency, not by source order.
var (
	a = note("a") + b
	b = note("b") + c
	c = note("c")
	d = func() int { return note("d") }()
)

func init() { note("init1") }
func init() { note("init2") }

type T struct {
	name string
	buf  [64]byte
}

var finalized []string

// Package variables only stored to: still GC roots, so their objects
// must never be finalized.
var sinkOnlyStored *T
var sinkSlice []*T
var sinkMap = map[int]*T{}
var sinkIface any

func set() {
	for _, n := range []string{"stored", "slice", "map", "iface"} {
		t := &T{name: n}
		runtime.SetFinalizer(t, func(t *T) { finalized = append(finalized, t.name) })
		switch n {
		case "stored":
			sinkOnlyStored = t
		case "slice":
			sinkSlice = append(sinkSlice, t)
		case "map":
			sinkMap[1] = t
		case "iface":
			sinkIface = t
		}
	}
	// One really unreachable object: its finalizer may run.
	u := &T{name: "unreachable"}
	runtime.SetFinalizer(u, func(t *T) {})
}

//go:linkname nanotime runtime.nanotime
func nanotime() int64

//go:noinline
func unusedButExported() string { return "x" }

func main() {
	fmt.Println(a, b, c, d, strings.Join(order, ","))
	set()
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
	fmt.Println("finalized:", finalized)
	fmt.Println(nanotime() > 0)
}
