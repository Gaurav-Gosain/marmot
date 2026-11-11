package main

import (
	"fmt"

	"github.com/shopspring/decimal"
)

func main() {
	a := decimal.RequireFromString("123.456")
	b := decimal.NewFromFloat(0.1)
	c := decimal.NewFromInt(3)
	fmt.Println(a.Add(b), a.Sub(b), a.Mul(c), a.Div(c), a.Div(c).Round(5), a.DivRound(c, 3))
	fmt.Println(a.Cmp(b), a.Equal(a), a.Sign(), a.Neg(), a.Abs(), a.Floor(), a.Ceil(), a.Truncate(1), a.Round(1), a.Round(-1))
	fmt.Println(a.StringFixed(5), a.StringFixed(1), a.Exponent(), a.Coefficient(), a.IntPart(), a.IsZero(), decimal.Zero.IsZero())
	x := decimal.NewFromFloat(0).Add(decimal.RequireFromString("0.1")).Add(decimal.RequireFromString("0.2"))
	fmt.Println(x, x.Equal(decimal.RequireFromString("0.3")), 0.1+0.2 == 0.3)
	for _, s := range []string{"1e3", "-0.00", "1.5E-3", "abc", "", "12.", ".5", "1_000", "99999999999999999999999.99999999"} {
		d, err := decimal.NewFromString(s)
		fmt.Println(s, d, err)
	}
	f, exact := a.Float64()
	fmt.Println(f, exact, a.InexactFloat64(), a.BigInt(), a.Pow(decimal.NewFromInt(2)))
	pw, _ := decimal.NewFromInt(2).PowInt32(-3)
	fmt.Println(pw, decimal.Sum(a, b, c), decimal.Max(a, b, c), decimal.Min(a, b, c), decimal.Avg(a, b, c))
	j, _ := a.MarshalJSON()
	fmt.Println(string(j))
	var back decimal.Decimal
	fmt.Println(back.UnmarshalJSON([]byte(`"7.25"`)), back, back.Shift(2), back.Shift(-3))
	q, r := a.QuoRem(c, 2)
	fmt.Println(q, r, a.Mod(c))
	total := decimal.Zero
	for i := 1; i <= 100; i++ {
		total = total.Add(decimal.NewFromInt(int64(i)).Div(decimal.NewFromInt(7)))
	}
	fmt.Println(total, total.Round(10))
	defer func() { fmt.Println("recovered:", recover()) }()
	decimal.NewFromInt(1).Div(decimal.Zero)
}
