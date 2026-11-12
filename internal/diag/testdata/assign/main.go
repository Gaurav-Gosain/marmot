package main

import (
	"fmt"
	"strconv"
)

func parse(s string) int {
	n := strconv.Atoi(s)
	return n
}

func pair() (int, int) { return 1, 2 }

func main() {
	fmt.Println(parse("1"), pair(1))
}
