package main

import "container/heap"

type Item struct {
	name  string
	pri   int
	index int
}

type PQ []*Item

func (p PQ) Len() int           { return len(p) }
func (p PQ) Less(i, j int) bool { return p[i].pri > p[j].pri }
func (p PQ) Swap(i, j int)      { p[i], p[j] = p[j], p[i]; p[i].index = i; p[j].index = j }
func (p *PQ) Push(x any)        { it := x.(*Item); it.index = len(*p); *p = append(*p, it) }
func (p *PQ) Pop() any {
	old := *p
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	*p = old[:n-1]
	return it
}

func main() {
	pq := PQ{}
	heap.Init(&pq)
	for i, n := range []string{"low", "mid", "high", "urgent", "meh"} {
		heap.Push(&pq, &Item{name: n, pri: []int{1, 5, 8, 10, 2}[i]})
	}
	it := pq[len(pq)-1]
	for _, x := range pq {
		if x.name == "mid" {
			it = x
		}
	}
	it.pri = 20
	heap.Fix(&pq, it.index)
	for pq.Len() > 0 {
		x := heap.Pop(&pq).(*Item)
		print(x.name, ":", x.pri, " ")
	}
	println()
}
