package main

import (
	"fmt"
	"math/big"
)

func main() {
	a, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	b := big.NewInt(987654321)
	fmt.Println(new(big.Int).Add(a, b), new(big.Int).Sub(b, a), new(big.Int).Mul(a, b))
	q, r := new(big.Int).QuoRem(a, b, new(big.Int))
	fmt.Println(q, r)
	d, m := new(big.Int).DivMod(big.NewInt(-7), big.NewInt(2), new(big.Int))
	fmt.Println(d, m, new(big.Int).Quo(big.NewInt(-7), big.NewInt(2)), new(big.Int).Rem(big.NewInt(-7), big.NewInt(2)))
	fmt.Println(a.Cmp(b), b.Cmp(a), a.Cmp(a), a.Sign(), new(big.Int).Neg(a).Sign(), new(big.Int).Abs(big.NewInt(-5)))
	fmt.Println(a.BitLen(), a.Bit(3), new(big.Int).Lsh(b, 70), new(big.Int).Rsh(a, 50), a.IsInt64(), b.Int64(), b.IsUint64())
	fmt.Println(a.String(), a.Text(16), a.Text(2)[:20], fmt.Sprintf("%x %d %s %v", b, b, b, b))
	_, ok := new(big.Int).SetString("12x", 10)
	fmt.Println(ok)
	z, _ := new(big.Int).SetString("-ff", 16)
	fmt.Println(z, z.Sign())
}
