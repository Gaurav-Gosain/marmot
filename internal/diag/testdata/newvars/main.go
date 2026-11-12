package main

import "fmt"

func main() {
	x := 1
	x := 2
	for i, v := range []int{1} {
		fmt.Println(v)
	}
	for k := range map[int]int{} {
	}
	fmt.Println(x)
}
