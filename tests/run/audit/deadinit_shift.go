// Audit (spec, 2026-11-12): an unused package variable whose initializer panics;
// dead initializer removal must keep the panic (gc panics during init).
package main

var zero = 0
var neg = -1
var short = []int{1}
var np *struct{ x int }
var iv any = "s"

func main() { println("main ran") }
var unused = 1 << neg
