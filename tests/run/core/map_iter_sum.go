package main

func main() {
	m := map[string]int{}
	keys := []string{"alpha", "beta", "gamma", "delta", "eps"}
	for i, k := range keys {
		m[k] = i + 1
	}
	total, kl := 0, 0
	for k, v := range m {
		total += v
		kl += len(k)
	}
	println(total, kl)
	big := map[int]int{}
	for i := 0; i < 10000; i++ {
		big[i*7] = i
	}
	s := 0
	for k, v := range big {
		s += k - 7*v
	}
	println(len(big), s)
	for i := 0; i < 10000; i += 2 {
		delete(big, i*7)
	}
	println(len(big), big[7], big[0])
}
