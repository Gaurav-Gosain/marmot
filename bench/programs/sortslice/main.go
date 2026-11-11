// sortslice: sort.Slice on structs and sort.Ints, plus slices-free binary search.
package main

import (
	"fmt"
	"sort"
)

const n = 4000000

type rec struct {
	key  int
	name string
}

func main() {
	x := uint64(7)
	recs := make([]rec, n)
	ints := make([]int, n)
	for i := range recs {
		x = x*6364136223846793005 + 1442695040888963407
		recs[i] = rec{int(x >> 40), fmt.Sprint("n", i%1000)}
		ints[i] = int(x >> 33)
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].key != recs[j].key {
			return recs[i].key < recs[j].key
		}
		return recs[i].name < recs[j].name
	})
	sort.Ints(ints)
	sum := 0
	for i := 0; i < n; i += 1000 {
		sum += recs[i].key ^ ints[i]
		sum += sort.SearchInts(ints, recs[i].key)
	}
	fmt.Println(sum, recs[0].key, ints[n-1])
}
