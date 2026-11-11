package main

import "math/rand"

func main() {
	r := rand.New(rand.NewSource(2024))
	for _, n := range []int{0, 1, 2, 7} {
		p := r.Perm(n)
		for _, v := range p {
			print(v, " ")
		}
		println()
	}
	words := []string{"a", "b", "c", "d", "e", "f"}
	r.Shuffle(len(words), func(i, j int) { words[i], words[j] = words[j], words[i] })
	for _, w := range words {
		print(w, " ")
	}
	println()
	defer func() { println(recover() != nil) }()
	r.Intn(0)
}
