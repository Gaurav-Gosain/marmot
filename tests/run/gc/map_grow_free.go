// Maps that grow through many rebuilds. A map that was never iterated gives
// its old table back at each rebuild; a map with an iteration started keeps
// it (the iterator may still read it). Values must survive collections, an
// iteration must see every old key once while inserts rebuild the table,
// and a clone must not share the original's table.
package main

import (
	"fmt"
	"maps"
	"runtime"
	"strconv"
)

func main() {
	// Never iterated: every rebuild frees the old table.
	m := map[int]int{}
	for i := 0; i < 300000; i++ {
		m[i*7+1] = i
		if i%50000 == 0 {
			runtime.GC()
		}
	}
	bad := 0
	for i := 0; i < 300000; i++ {
		if v, ok := m[i*7+1]; !ok || v != i {
			bad++
		}
	}
	fmt.Println("plain", len(m), "bad", bad)

	// Inserts during an iteration rebuild the table under the iterator.
	it := map[int]int{}
	for i := 0; i < 1000; i++ {
		it[i] = i
	}
	seen := map[int]int{}
	next := 1000000
	steps := 0
	for k := range it {
		if k < 1000 {
			seen[k]++
		}
		// Inserted keys may be produced by this loop too: insert only in
		// the first steps, enough for several rebuilds.
		if steps < 100 {
			for j := 0; j < 64; j++ {
				it[next] = next
				next++
			}
		}
		steps++
	}
	once := 0
	for k := 0; k < 1000; k++ {
		if seen[k] == 1 {
			once++
		}
	}
	fmt.Println("iter old keys seen once", once, "len", len(it))

	// A clone, then the original grows.
	src := map[string]string{}
	for i := 0; i < 2000; i++ {
		src["k"+strconv.Itoa(i)] = "v" + strconv.Itoa(i)
	}
	c := maps.Clone(src)
	for i := 2000; i < 200000; i++ {
		src["k"+strconv.Itoa(i)] = "v" + strconv.Itoa(i)
	}
	runtime.GC()
	cb := 0
	for i := 0; i < 2000; i++ {
		if c["k"+strconv.Itoa(i)] != "v"+strconv.Itoa(i) {
			cb++
		}
	}
	fmt.Println("clone", len(c), "bad", cb, "src", len(src))

	// Pointer values across rebuilds and collections.
	ps := map[string]*[4]int{}
	for i := 0; i < 100000; i++ {
		ps[strconv.Itoa(i)] = &[4]int{i, i + 1, i + 2, i + 3}
		if i%20000 == 0 {
			runtime.GC()
		}
	}
	runtime.GC()
	pb := 0
	for i := 0; i < 100000; i++ {
		p := ps[strconv.Itoa(i)]
		if p == nil || p[0] != i || p[3] != i+3 {
			pb++
		}
	}
	fmt.Println("pointers", len(ps), "bad", pb)

	// A large value copied out, then inserts that rebuild the table: the
	// copy must not change (it does not point into the table).
	type big struct {
		a [40]int
		s string
	}
	bm := map[int]big{}
	for i := 0; i < 8; i++ {
		bm[i] = big{s: "x" + strconv.Itoa(i)}
		bm[i] = big{a: [40]int{i, i}, s: bm[i].s}
	}
	v := bm[3]
	for i := 8; i < 50000; i++ {
		bm[i] = big{a: [40]int{-1}, s: "y"}
	}
	runtime.GC()
	fmt.Println("copy", v.a[0], v.a[1], v.s, len(bm))
}
