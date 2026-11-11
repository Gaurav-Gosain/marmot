// Audit (spec, 2026-11-12): an unused package variable whose initializer panics;
// dead initializer removal must keep the panic (gc panics during init).
package main

var k any = []int{1}
var unused = map[any]int{k: 1}

func main() { println("main ran") }
