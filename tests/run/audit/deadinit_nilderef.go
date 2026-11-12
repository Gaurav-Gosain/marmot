// Dead initializer removal drops the initializer of an
// unused package variable even though it panics (removeDeadInit counts
// IndexAddr/Index, loads through pointers and shifts as removable). gc runs
// it and the program panics during init; marmot runs main.
package main

var zero = 0
var neg = -1
var short = []int{1}
var np *struct{ x int }
var iv any = "s"

func main() { println("main ran") }
var unused = np.x
