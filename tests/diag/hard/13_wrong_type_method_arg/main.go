package main

import "fmt"

func main() {
	l := &Logger{}
	l.Write("started")
	l.Write("done")
	fmt.Println(l.Count())
}
