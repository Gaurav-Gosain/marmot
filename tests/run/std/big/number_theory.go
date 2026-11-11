package main

import (
	"fmt"
	"math/big"
)

func main() {
	f := big.NewInt(1)
	for i := int64(2); i <= 50; i++ {
		f.Mul(f, big.NewInt(i))
	}
	fmt.Println(f, new(big.Int).MulRange(1, 50).Cmp(f))
	fmt.Println(new(big.Int).Exp(big.NewInt(2), big.NewInt(200), nil))
	fmt.Println(new(big.Int).Exp(big.NewInt(3), big.NewInt(1000), big.NewInt(1000000007)))
	fmt.Println(new(big.Int).GCD(nil, nil, big.NewInt(462), big.NewInt(1071)))
	x, y := new(big.Int), new(big.Int)
	g := new(big.Int).GCD(x, y, big.NewInt(240), big.NewInt(46))
	fmt.Println(g, x, y)
	fmt.Println(new(big.Int).ModInverse(big.NewInt(3), big.NewInt(11)), new(big.Int).Sqrt(big.NewInt(1000000)))
	for _, n := range []int64{1, 2, 17, 91, 7919, 1000003, 1000001} {
		fmt.Print(big.NewInt(n).ProbablyPrime(10), " ")
	}
	fmt.Println()
	p, _ := new(big.Int).SetString("170141183460469231731687303715884105727", 10)
	fmt.Println(p.ProbablyPrime(20), p.BitLen())
	fib := func(n int) *big.Int {
		a, b := big.NewInt(0), big.NewInt(1)
		for i := 0; i < n; i++ {
			a.Add(a, b)
			a, b = b, a
		}
		return a
	}
	fmt.Println(fib(300), len(fib(1000).String()))
	fmt.Println(new(big.Int).Binomial(60, 30))
}
