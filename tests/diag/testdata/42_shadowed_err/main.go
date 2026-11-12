package main

import "os"

func load() error {
	var err error
	if true {
		data, err := os.ReadFile("x")
		_ = data
	}
	return err
}

func main() { println(load() != nil) }
