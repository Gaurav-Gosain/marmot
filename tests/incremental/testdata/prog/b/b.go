// Package b uses a; its module must follow a's edits exactly when needed.
package b

import (
	"fmt"
	"strconv"

	"incrtest/a"
)

// Run exercises a's API, generics and summaries.
func Run() (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = fmt.Sprint("recovered: ", r)
		}
	}()
	x := 5
	n := a.Keep(&x)
	var bx a.Box
	bx.Push(1)
	bx.Push(2)
	ys := a.Map([]int{1, 2, 3}, func(i int) string { return strconv.Itoa(i * a.C) })
	t := a.T{X: 4, Y: "y"}
	return fmt.Sprint(n, " ", bx.Len(), " ", ys, " ", a.V, " ", t, " ", a.MayPanic(-1), " ", a.Recov())
}
