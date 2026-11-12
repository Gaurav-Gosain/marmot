package b

import (
	_ "unsafe"

	"marmotworld/audit_linkname_var/c"
)

//go:linkname helper marmotworld/audit_linkname_var/c.Helper
func helper(x int) int { return x*x + B }

var B = c.Note("B") + len(c.Trace)

var Obj = &struct{ N [32]int }{}

func init() {
	c.Note("b.init")
	Obj.N[5] = 55
}
