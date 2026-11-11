package main

func main() {
	m := map[int]int{}
	for i := 0; i < 100; i++ {
		m[i] = i
	}
	n := 0
	for k := range m {
		delete(m, k)
		n++
	}
	println(len(m), n)
	for i := 0; i < 50; i++ {
		m[i] = i
	}
	cnt := 0
	for k := range m {
		if k%2 == 0 {
			delete(m, k)
		}
		cnt++
	}
	println(len(m))
	sum := 0
	for _, v := range m {
		sum += v
	}
	println(sum)
	seen := 0
	for k := range m {
		delete(m, k+1)
		seen++
	}
	println(seen >= 1, len(m) < 25)
	var nm map[int]int
	for range nm {
		println("never")
	}
}
