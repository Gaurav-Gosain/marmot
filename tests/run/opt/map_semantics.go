package main

import (
	"fmt"
	"sort"
)

type K struct {
	A int8
	B string
}

func main() {
	m := map[int32]int{}
	for i := 0; i < 1000; i++ {
		m[int32(i*7%101)] += i
	}
	keys := make([]int, 0)
	for k := range m {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	fmt.Println(len(m), keys[:5], m[0], m[100])
	sm := map[string]int{}
	for i := 0; i < 500; i++ {
		sm[fmt.Sprint("k", i%37)]++
	}
	fmt.Println(len(sm), sm["k0"], sm["k36"], sm["none"])
	km := map[K][]int{}
	for i := 0; i < 30; i++ {
		k := K{int8(i % 3), fmt.Sprint(i % 2)}
		km[k] = append(km[k], i)
	}
	fmt.Println(len(km), km[K{0, "0"}], km[K{1, "1"}])
	fm := map[float64]int{}
	nan := 0.0
	nan /= nan
	fm[nan]++
	fm[nan]++
	fm[0.0]++
	negz := 0.0
	negz = -negz
	fm[negz]++
	fmt.Println(len(fm), fm[0])
	im := map[any]int{1: 1, "1": 2, 1.0: 3, int8(1): 4, K{1, "x"}: 5}
	fmt.Println(len(im), im[1], im["1"], im[1.0], im[int8(1)], im[K{1, "x"}])
	delete(m, 5)
	delete(m, 5)
	_, ok := m[5]
	fmt.Println(ok, len(m))
	big := map[uint64]uint64{}
	for i := uint64(0); i < 5000; i++ {
		big[i*0x9e3779b97f4a7c15] = i
	}
	hit := 0
	for i := uint64(0); i < 5000; i++ {
		if big[i*0x9e3779b97f4a7c15] == i {
			hit++
		}
	}
	fmt.Println(len(big), hit)
	defer func() { fmt.Println(recover()) }()
	var nm map[string]int
	nm["x"]++
}
