package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(strings.Repeat("a", 2), filepath.Join("a", "b"))
	t := tmplate.New("x")
	_ = t
}
