// Edge cases for backend optimizations; the expected output is from gc.
package main

import (
	"container/list"
	"container/ring"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

type box struct{ v [4]int }

var (
	pool   sync.Pool
	lst    = list.New()
	ap     atomic.Pointer[box]
	av     atomic.Value
	m      sync.Map
	ctx    context.Context
	rg     = ring.New(1)
	errw   error
	sb     strings.Builder
	keep   []*box
	upKeep unsafe.Pointer
)

type key struct{}

//go:noinline
func clobber() int {
	var buf [512]int
	for i := range buf {
		buf[i] = -7
	}
	s := 0
	for _, v := range buf {
		s += v
	}
	return s
}

type wrapErr struct{ b *box }

func (w wrapErr) Error() string { return fmt.Sprint(w.b.v) }

type byV []*box

func (s byV) Len() int           { return len(s) }
func (s byV) Less(i, j int) bool { return s[i].v[0] < s[j].v[0] }
func (s byV) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

//go:noinline
func fill() {
	b1 := &box{[4]int{1, 1, 1, 1}}
	lst.PushBack(b1)
	b2 := &box{[4]int{2, 2, 2, 2}}
	ap.Store(b2)
	b3 := &box{[4]int{3, 3, 3, 3}}
	av.Store(b3)
	b4 := &box{[4]int{4, 4, 4, 4}}
	m.Store("k", b4)
	b5 := &box{[4]int{5, 5, 5, 5}}
	ctx = context.WithValue(context.Background(), key{}, b5)
	b6 := &box{[4]int{6, 6, 6, 6}}
	rg.Value = b6
	b7 := &box{[4]int{7, 7, 7, 7}}
	errw = fmt.Errorf("w: %w", wrapErr{b7})
	b8 := &box{[4]int{8, 8, 8, 8}}
	pool.Put(b8)
	b9 := &box{[4]int{9, 9, 9, 9}}
	s := byV{b9}
	sort.Sort(s)
	keep = s
	b10 := &box{[4]int{10, 10, 10, 10}}
	atomic.StorePointer(&upKeep, unsafe.Pointer(b10))
	b11 := &box{[4]int{11, 11, 11, 11}}
	runtime.KeepAlive(b11)
	keep = append(keep, b11)
}

func main() {
	fill()
	clobber()
	runtime.GC()
	clobber()
	fmt.Println(lst.Front().Value.(*box).v, ap.Load().v, av.Load().(*box).v)
	v, _ := m.Load("k")
	fmt.Println(v.(*box).v, ctx.Value(key{}).(*box).v, rg.Value.(*box).v)
	var we wrapErr
	fmt.Println(errors.As(errw, &we), we.b.v)
	if p, ok := pool.Get().(*box); ok {
		fmt.Println("pool", p.v)
	} else {
		fmt.Println("pool", [4]int{8, 8, 8, 8})
	}
	fmt.Println(keep[0].v, keep[1].v, (*box)(atomic.LoadPointer(&upKeep)).v)
}
