package main

/*
#include <string.h>
*/
import "C"

func main() {
	s := "hello"
	println(C.strlen(s))
}
