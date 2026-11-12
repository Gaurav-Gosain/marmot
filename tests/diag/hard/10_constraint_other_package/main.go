package main

import (
	"fmt"

	"example.com/app/mathx"
)

func main() {
	prices := []float32{1.5, 2.5}
	fmt.Println(mathx.Sum(prices))
}
