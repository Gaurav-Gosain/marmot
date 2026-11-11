package main

import (
	"sync"
	"sync/atomic"
)

type node struct {
	v    int
	next *node
}

type Stack struct{ head atomic.Pointer[node] }

func (s *Stack) Push(v int) {
	n := &node{v: v}
	for {
		old := s.head.Load()
		n.next = old
		if s.head.CompareAndSwap(old, n) {
			return
		}
	}
}

func (s *Stack) Pop() (int, bool) {
	for {
		old := s.head.Load()
		if old == nil {
			return 0, false
		}
		if s.head.CompareAndSwap(old, old.next) {
			return old.v, true
		}
	}
}

func main() {
	var s Stack
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 250; i++ {
				s.Push(g*1000 + i)
			}
		}()
	}
	wg.Wait()
	n, sum := 0, 0
	for {
		v, ok := s.Pop()
		if !ok {
			break
		}
		n++
		sum += v
	}
	println(n, sum)
}
