package main

import "os"

func main() {
	data, err := os.ReadFile("x")
	_ = data
}
