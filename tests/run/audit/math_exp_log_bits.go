// math.Exp and math.Log results that differ from gc on arm64 in the last
// bit. Exp: gc uses exp_arm64.s; marmot's overlay uses the pure Go exp.go,
// which differs at the underflow edge. Log: both use log.go; gc fuses
// s*(hfsq+R) + k*Ln2Lo as FMA(s, hfsq+R, k*Ln2Lo), marmot does not (for a
// plain a*b + c*d both fuse the second product).
package main

import (
	"fmt"
	"math"
)

//go:noinline
func bits(u uint64) float64 { return math.Float64frombits(u) }

func main() {
	for _, u := range []uint64{0xc087490a3d70a3d7, 0xc08748916872b021, 0xc087489f80dbf28c} {
		x := bits(u)
		fmt.Printf("Exp(%v) = %#x\n", x, math.Float64bits(math.Exp(x)))
	}
	x := bits(0x40666da9fbe76c8b)
	fmt.Printf("Log(%v) = %#x\n", x, math.Float64bits(math.Log(x)))
}
