// Three packages: go:linkname push (b implements c.Helper), pull of a
// function (a.hiddenFunc = c.hiddenFunc) and of a variable (a.hidden =
// c.hidden), and init order by dependency across packages. gc prints
// "2000 hiddenFunc".
package main

import (
	"fmt"
	"runtime"

	"marmotworld/audit_linkname_var/a"
	"marmotworld/audit_linkname_var/b"
	"marmotworld/audit_linkname_var/c"
)

func init() { c.Note("main.init") }

func main() {
	fmt.Println(a.A, a.Later, b.B, c.First, c.Second)
	fmt.Println(a.Hidden())
	fmt.Println(c.Helper(4))
	runtime.GC()
	fmt.Println(b.Obj.N[5])
	for _, t := range c.Trace {
		fmt.Println(t)
	}
}
