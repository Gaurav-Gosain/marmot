package main

/*
static int add(int a, int b) { return a + b }
*/
import "C"

func main() { println(C.add(1, 2)) }
