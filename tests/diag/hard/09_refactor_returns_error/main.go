package main

import (
	"fmt"

	"example.com/app/parse"
)

func total(s string) int {
	t := 0
	for _, v := range parse.Fields(s) {
		t += v
	}
	return t
}

func main() {
	fmt.Println(total("a=1,b=2"))
}
