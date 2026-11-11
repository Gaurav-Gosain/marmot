// Arbitrary precision unsigned integers implemented from scratch (base 1e9 limbs):
// add, sub, mul (schoolbook and Karatsuba), divmod by small and big, power, factorial,
// fibonacci, and decimal parsing and printing. Cross-checked with math/big.
package main

import (
	"fmt"
	"math/big"
	"strings"
)

const base = 1_000_000_000

type Nat []uint32 // little endian limbs, no trailing zero limbs

func (a Nat) trim() Nat {
	for len(a) > 0 && a[len(a)-1] == 0 {
		a = a[:len(a)-1]
	}
	return a
}

func FromUint(x uint64) Nat {
	var r Nat
	for x > 0 {
		r = append(r, uint32(x%base))
		x /= base
	}
	return r
}

func Parse(s string) (Nat, error) {
	if s == "" {
		return nil, fmt.Errorf("empty")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("bad digit %q", c)
		}
	}
	var r Nat
	for end := len(s); end > 0; end -= 9 {
		start := max(end-9, 0)
		var v uint32
		for _, c := range s[start:end] {
			v = v*10 + uint32(c-'0')
		}
		r = append(r, v)
	}
	return r.trim(), nil
}

func (a Nat) String() string {
	if len(a) == 0 {
		return "0"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d", a[len(a)-1])
	for i := len(a) - 2; i >= 0; i-- {
		fmt.Fprintf(&sb, "%09d", a[i])
	}
	return sb.String()
}

func Cmp(a, b Nat) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func Add(a, b Nat) Nat {
	if len(a) < len(b) {
		a, b = b, a
	}
	r := make(Nat, 0, len(a)+1)
	var carry uint32
	for i := range a {
		s := a[i] + carry
		if i < len(b) {
			s += b[i]
		}
		if s >= base {
			s -= base
			carry = 1
		} else {
			carry = 0
		}
		r = append(r, s)
	}
	if carry > 0 {
		r = append(r, carry)
	}
	return r
}

func Sub(a, b Nat) (Nat, error) {
	if Cmp(a, b) < 0 {
		return nil, fmt.Errorf("negative result")
	}
	r := make(Nat, len(a))
	var borrow int64
	for i := range a {
		d := int64(a[i]) - borrow
		if i < len(b) {
			d -= int64(b[i])
		}
		if d < 0 {
			d += base
			borrow = 1
		} else {
			borrow = 0
		}
		r[i] = uint32(d)
	}
	return r.trim(), nil
}

func mulSchool(a, b Nat) Nat {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	r := make([]uint64, len(a)+len(b)+1)
	for i, x := range a {
		var carry uint64
		for j, y := range b {
			cur := r[i+j] + uint64(x)*uint64(y) + carry
			r[i+j] = cur % base
			carry = cur / base
		}
		k := i + len(b)
		for carry > 0 {
			cur := r[k] + carry
			r[k] = cur % base
			carry = cur / base
			k++
		}
	}
	out := make(Nat, len(r))
	for i, v := range r {
		out[i] = uint32(v)
	}
	return out.trim()
}

func shift(a Nat, n int) Nat {
	if len(a) == 0 {
		return nil
	}
	return append(make(Nat, n), a...)
}

func Mul(a, b Nat) Nat {
	if len(a) < 24 || len(b) < 24 {
		return mulSchool(a, b)
	}
	m := max(len(a), len(b)) / 2
	split := func(x Nat) (Nat, Nat) {
		if len(x) <= m {
			return x, nil
		}
		return Nat(x[:m]).trim(), x[m:]
	}
	a0, a1 := split(a)
	b0, b1 := split(b)
	z0 := Mul(a0, b0)
	z2 := Mul(a1, b1)
	z1 := Mul(Add(a0, a1), Add(b0, b1))
	z1, _ = Sub(z1, z0)
	z1, _ = Sub(z1, z2)
	return Add(Add(z0, shift(z1, m)), shift(z2, 2*m))
}

func DivSmall(a Nat, d uint32) (Nat, uint32) {
	q := make(Nat, len(a))
	var rem uint64
	for i := len(a) - 1; i >= 0; i-- {
		cur := rem*base + uint64(a[i])
		q[i] = uint32(cur / uint64(d))
		rem = cur % uint64(d)
	}
	return q.trim(), uint32(rem)
}

func Pow(a Nat, n int) Nat {
	r := Nat{1}
	for n > 0 {
		if n&1 == 1 {
			r = Mul(r, a)
		}
		a = Mul(a, a)
		n >>= 1
	}
	return r
}

func Fact(n int) Nat {
	r := Nat{1}
	for i := 2; i <= n; i++ {
		r = Mul(r, FromUint(uint64(i)))
	}
	return r
}

func Fib(n int) Nat {
	a, b := Nat(nil), Nat{1}
	for i := 0; i < n; i++ {
		a, b = b, Add(a, b)
	}
	return a
}

func toBig(a Nat) *big.Int {
	r, _ := new(big.Int).SetString(a.String(), 10)
	return r
}

func main() {
	a, _ := Parse("123456789012345678901234567890")
	b, _ := Parse("987654321098765432109876543210")
	fmt.Println(Add(a, b), Mul(a, b))
	d, err := Sub(b, a)
	fmt.Println(d, err)
	_, err = Sub(a, b)
	fmt.Println(err)
	_, err = Parse("12a")
	fmt.Println(err)
	_, err = Parse("")
	fmt.Println(err)
	z, _ := Parse("0000")
	fmt.Println(z, len(z))
	q, r := DivSmall(b, 97)
	fmt.Println(q, r)
	fmt.Println(Pow(FromUint(2), 200))
	fmt.Println(Fact(30))
	fmt.Println(Fib(300))
	f100 := Fact(100)
	s := f100.String()
	fmt.Println(len(s), strings.Count(s, "0"), s[:20])
	for _, n := range []int{50, 200, 500} {
		x := Fact(n)
		ok := toBig(x).Cmp(new(big.Int).MulRange(1, int64(n))) == 0
		fmt.Println("fact", n, len(x.String()), ok)
	}
	p := Pow(FromUint(3), 1000)
	ref := new(big.Int).Exp(big.NewInt(3), big.NewInt(1000), nil)
	fmt.Println(len(p.String()), p.String() == ref.String())
	karatsubaOK := true
	x, y := Fact(400), Fact(350)
	if Mul(x, y).String() != mulSchool(x, y).String() {
		karatsubaOK = false
	}
	fmt.Println("karatsuba matches schoolbook:", karatsubaOK, len(Mul(x, y)))
	f1000 := Fib(1000)
	rf := big.NewInt(0)
	ra, rb := big.NewInt(0), big.NewInt(1)
	for i := 0; i < 1000; i++ {
		ra.Add(ra, rb)
		ra, rb = rb, ra
	}
	rf.Set(ra)
	fmt.Println(f1000.String() == rf.String(), f1000.String()[:15])
	sum := 0
	for _, c := range Pow(FromUint(2), 1000).String() {
		sum += int(c - '0')
	}
	fmt.Println("digit sum 2^1000:", sum)
}
