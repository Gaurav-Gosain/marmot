package main

import (
	"fmt"
	"math/big"
)

func main() {
	r := big.NewRat(3, 4)
	s := big.NewRat(5, 6)
	fmt.Println(new(big.Rat).Add(r, s), new(big.Rat).Mul(r, s), new(big.Rat).Quo(r, s), new(big.Rat).Sub(r, s))
	fmt.Println(r.Cmp(s), r.FloatString(5), s.FloatString(3), big.NewRat(10, 4), big.NewRat(-6, 3).IsInt())
	f, exact := r.Float64()
	fmt.Println(f, exact)
	x, _ := new(big.Rat).SetString("0.125")
	fmt.Println(x, x.Num(), x.Denom())
	sum := new(big.Rat)
	for i := int64(1); i <= 20; i++ {
		sum.Add(sum, big.NewRat(1, i))
	}
	fmt.Println(sum, sum.FloatString(10))
	a := new(big.Float).SetPrec(100).SetInt64(2)
	sq := new(big.Float).SetPrec(100).Sqrt(a)
	fmt.Println(sq.Text('f', 30), sq.Prec())
	h := big.NewFloat(1.5)
	h.Mul(h, big.NewFloat(4))
	fmt.Println(h, h.Cmp(big.NewFloat(6)), h.Sign())
	fmt.Printf("%.5f %g\n", new(big.Float).Quo(big.NewFloat(1), big.NewFloat(3)), big.NewFloat(1e100))
}
