// A pointer moved from a heap slot into a just-allocated object, then the
// slot overwritten, while a concurrent mark runs: the moved object must
// survive. With card marking, a store into a fresh object needs its card
// (an object allocated during the mark is marked and not scanned again
// unless its card is dirty). Meant for conform -modes cards (MARMOT_WB=cards,
// MARMOT_GC_CONC=1, MARMOT_GC_VERIFY=1): without that card, gc verify
// reports reachable objects left unmarked.
package main

import (
	"fmt"
	"sync"
)

type node struct {
	v   int
	pad [6]int
}

type holder struct {
	p   *node
	idx int
}

const nslots = 1 << 21

// A heap array, not a global: the first stop of a cycle marks it but does
// not scan it, so its elements are reached only while the mark runs beside
// the program.
var slots []*node

const nkeep = 1 << 12

//go:noinline
func move(i int, keep []*holder, k int) {
	p := slots[i]
	h := &holder{p: p, idx: p.v}
	slots[i] = &node{v: p.v}
	keep[k] = h
}

func worker(w int, bad *int, wg *sync.WaitGroup) {
	defer wg.Done()
	keep := make([]*holder, nkeep)
	x := uint32(12345 + w*7919)
	for round := 0; round < 200; round++ {
		for k := range keep {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			// Each worker owns a quarter of the slots.
			move(int(x%(nslots/4))*4+w, keep, k)
		}
		for _, h := range keep {
			if h.p.v != h.idx {
				*bad++
			}
		}
	}
}

func main() {
	slots = make([]*node, nslots)
	for i := range slots {
		slots[i] = &node{v: i}
	}
	var wg sync.WaitGroup
	bad := make([]int, 4)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go worker(w, &bad[w], &wg)
	}
	wg.Wait()
	fmt.Println("bad", bad[0]+bad[1]+bad[2]+bad[3])
}
