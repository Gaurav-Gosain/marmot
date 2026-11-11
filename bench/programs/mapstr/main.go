// map insert, lookup and delete with string keys built by hand.
package main

const n = 1000000

func key(i int) string {
	var b [12]byte
	p := len(b)
	for {
		p--
		b[p] = byte('a' + i%26)
		i /= 26
		if i == 0 {
			break
		}
	}
	return "k" + string(b[p:])
}

func main() {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = key(i * 31)
	}
	m := map[string]int{}
	for i, k := range keys {
		m[k] = i
	}
	hits, sum := 0, 0
	for r := 0; r < 14; r++ {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				hits++
				sum += v
			}
		}
	}
	for i := 0; i < n; i += 3 {
		delete(m, keys[i])
	}
	println("mapstr", len(m), hits, sum)
}
