// cgo: C global variables, read and written from Go.
package main

/*
int counter = 7;
double scale = 1.5;
int table[4] = {10, 20, 30, 40};
const char *label = "label";
static int bumpcounter(void) { return ++counter; }
static int tablesum(void) { int s = 0; for (int i = 0; i < 4; i++) s += table[i]; return s; }
*/
import "C"

import "fmt"

func main() {
	fmt.Println(C.counter, float64(C.scale), C.table, C.GoString(C.label))
	C.counter = 100
	fmt.Println(C.bumpcounter(), C.counter)
	C.table[2] = 300
	fmt.Println(C.tablesum())
	p := &C.counter
	*p += 5
	fmt.Println(C.counter)
}
