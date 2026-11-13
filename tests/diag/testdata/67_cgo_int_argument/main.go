package main

/*
static int add(int a, int b) { return a + b; }
*/
import "C"

func main() {
	x := 3
	println(C.add(x, 2))
}
