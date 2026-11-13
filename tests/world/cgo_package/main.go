// A module whose package uses cgo with its own C and assembly files.
package main

import (
	"fmt"

	"marmotworld/cgo_package/clib"
)

func main() {
	var s clib.Stats
	for i := 1; i <= 10; i++ {
		s.Add(float64(i))
	}
	fmt.Println(s.Count(), s.Mean())
	fmt.Println(clib.Reverse("marmot"), clib.Reverse(""), clib.Reverse("ab"))
	fmt.Println(clib.Scaled(14), clib.AsmAdd(40, 2), clib.TwicePlusOne(20))
}
