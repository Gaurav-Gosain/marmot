package main

import "fmt"

func main() {
	fmt.Println()
	fmt.Println("a", 1, 2.5, true, nil, 'x', "b")
	fmt.Print("a", "b", 1, 2, "c", 3.5, "\n")
	fmt.Print(1, 2, "\n")
	fmt.Print("x", 1, "\n")
	fmt.Println(fmt.Sprint(), fmt.Sprint(nil), fmt.Sprint("a", "b"), fmt.Sprint(1, 2), fmt.Sprint(1, "a", 2))
	fmt.Println(fmt.Sprintln("a", 1) == "a 1\n", fmt.Sprintln() == "\n")
	var i interface{}
	var p *int
	var e error
	var m map[string]int
	var s []int
	var f func()
	fmt.Println(i, p, e, m, s, f == nil)
	fmt.Printf("%v %v %v %v %d\n", i, p, e, m, s)
	fmt.Println(int8(-5), uint8(5), uintptr(10), complex(1, -2), complex64(complex(0.5, 0)))
}
