// map insert, lookup and delete with int keys.
package main

const n = 3000000

func main() {
	m := map[int]int{}
	for i := 0; i < n; i++ {
		m[i*7919%1000003+i] = i
	}
	hits, sum := 0, 0
	for r := 0; r < 5; r++ {
		for i := 0; i < n; i++ {
			if v, ok := m[i*7919%1000003+i]; ok {
				hits++
				sum += v
			}
		}
	}
	for i := 0; i < n; i += 2 {
		delete(m, i*7919%1000003+i)
	}
	println("mapint", len(m), hits, sum)
}
