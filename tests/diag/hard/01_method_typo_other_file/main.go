package main

import "fmt"

func main() {
	var l List
	l.Add("a")
	l.Add("b")
	fmt.Println(l.Lenght(), l.Contain("a"))
}
