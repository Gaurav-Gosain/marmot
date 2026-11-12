// An unused package variable whose initializer panics;
// dead initializer removal must keep the panic (gc panics during init).
package main

var unused = make([]byte, 1<<62)

func main() { println("main ran") }
