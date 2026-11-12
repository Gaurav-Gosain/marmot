// Audit (loops, 2026-11-12): three packages with go:linkname push (b
// implements c.Helper), pull of a function (a.hiddenFunc = c.hiddenFunc) and
// pull of a variable (a.hidden = c.hidden), plus init order by dependency
// across packages. gc prints "2000 hiddenFunc"; marmot (dev and -release)
// prints "0 hiddenFunc": the pulled variable is not bound to c.hidden.
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
