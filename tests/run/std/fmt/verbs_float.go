package main

import (
	"fmt"
	"math"
)

func main() {
	for _, v := range []float64{0, 1, -1.5, 3.14159265, 1e6, 1e-5, 123456789.0, 1e21, math.Inf(1), math.Inf(-1), math.NaN()} {
		fmt.Printf("%v|%f|%.2f|%8.3f|%-8.1f|%e|%E|%g|%G|%+.1f\n", v, v, v, v, v, v, v, v, v, v)
	}
	fmt.Printf("%6.2f|%06.2f|%+06.2f|% .2f\n", 3.14159, 3.14159, 3.14159, 3.14159)
	fmt.Printf("%.0f %.0f %.0f %.1f\n", 0.5, 1.5, 2.5, 0.05)
	fmt.Printf("%v %v\n", float32(0.1), float32(1)/3)
	fmt.Printf("%g %g %g\n", 100000.0, 1000000.0, 1e-4)
	fmt.Printf("%.3g|%.10g|%10.4g|\n", 1234.5678, 1.0/3, 1234.5678)
}
