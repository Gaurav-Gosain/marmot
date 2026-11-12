package main

func main() {
	n := 0
	for k := range map[string]int{"a": 1} {
		n++
	}
	println(n)
}
