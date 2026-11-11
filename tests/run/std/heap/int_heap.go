package main

import "container/heap"

type IntHeap []int

func (h IntHeap) Len() int           { return len(h) }
func (h IntHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h IntHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *IntHeap) Push(x any)        { *h = append(*h, x.(int)) }
func (h *IntHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func main() {
	h := &IntHeap{5, 2, 8, 1, 9, 3}
	heap.Init(h)
	heap.Push(h, 0)
	heap.Push(h, 7)
	println((*h)[0], h.Len())
	heap.Remove(h, 3)
	for h.Len() > 0 {
		print(heap.Pop(h).(int), " ")
	}
	println()
	x := uint32(1)
	big := &IntHeap{}
	for i := 0; i < 5000; i++ {
		x = x*1664525 + 1013904223
		heap.Push(big, int(x>>10))
	}
	prev, ok := -1, true
	for big.Len() > 0 {
		v := heap.Pop(big).(int)
		if v < prev {
			ok = false
		}
		prev = v
	}
	println(ok)
}
