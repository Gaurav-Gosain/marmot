// Audit (spec, 2026-11-12): the FMA rule fuses where gc fuses on arm64,
// but gc also fuses inside complex multiplication: the real part of
// (a+bi)*(c+di) is computed as fma(a, c, -(b*d)). With a == b and c == d
// the unfused result is 0 and the fused one is the rounding error of a*c.
// On amd64 gc does not fuse (both 0).
package main

import (
	"fmt"
	"math"
)

var x, y = 0.1, 10.0

func main() {
	cc := complex(x, x) * complex(y, y)
	fmt.Printf("%b %b\n", math.Float64bits(real(cc)), math.Float64bits(imag(cc)))
	var c64 complex64 = complex(float32(x), float32(x)) * complex(float32(y), float32(y))
	fmt.Printf("%b %b\n", math.Float32bits(real(c64)), math.Float32bits(imag(c64)))
}
