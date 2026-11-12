package main

func find(xs []int, v int) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
}

func main() { println(find(nil, 1)) }
