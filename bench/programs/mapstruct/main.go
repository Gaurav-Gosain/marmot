// mapstruct: maps with struct values and array keys.
package main

import "fmt"

const n = 300000

type rec struct {
	count int
	sum   float64
	name  string
}

func main() {
	byName := map[string]rec{}
	byPos := map[[2]int]rec{}
	names := make([]string, 5000)
	for i := range names {
		names[i] = fmt.Sprint("name", i)
	}
	for r := 0; r < 20; r++ {
		for i := 0; i < n; i++ {
			k := names[(i*7)%len(names)]
			v := byName[k]
			v.count++
			v.sum += float64(i % 10)
			v.name = k
			byName[k] = v
			p := [2]int{i % 701, i % 13}
			w := byPos[p]
			w.count += 2
			w.sum += 0.5
			byPos[p] = w
		}
	}
	tot := 0.0
	cnt := 0
	for _, v := range byName {
		tot += v.sum
		cnt += v.count
	}
	for _, v := range byPos {
		tot += v.sum
		cnt += v.count
	}
	fmt.Println(len(byName), len(byPos), cnt, int64(tot))
}
