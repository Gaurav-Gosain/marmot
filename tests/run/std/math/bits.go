package main

import "math/bits"

func main() {
	println(bits.OnesCount(255), bits.OnesCount64(1<<63|1), bits.LeadingZeros32(1), bits.LeadingZeros64(0), bits.TrailingZeros(8), bits.TrailingZeros64(0))
	println(bits.Len(0), bits.Len(255), bits.Len64(1<<40))
	println(bits.RotateLeft32(0x80000001, 1), bits.RotateLeft64(1, -1), bits.Reverse8(1), bits.ReverseBytes32(0x01020304))
	hi, lo := bits.Mul64(1<<63, 4)
	println(hi, lo)
	sum, carry := bits.Add64(1<<63, 1<<63, 1)
	println(sum, carry)
	diff, borrow := bits.Sub64(0, 1, 0)
	println(diff, borrow)
	q, r := bits.Div64(1, 0, 3)
	println(q, r)
	println(bits.UintSize)
}
