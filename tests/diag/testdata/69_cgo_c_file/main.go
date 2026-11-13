package main

// int twice(int);
import "C"

func main() { println(C.twice(2)) }
