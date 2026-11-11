// A generic matrix library over numeric types: arithmetic, transpose, determinant,
// inverse (float), power, and rational numbers as an alternative element type.
package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

type Number interface {
	~int | ~int32 | ~int64 | ~float32 | ~float64
}

type Matrix[T Number] struct {
	r, c int
	d    []T
}

var ErrShape = errors.New("shape mismatch")

func New[T Number](r, c int) *Matrix[T] { return &Matrix[T]{r, c, make([]T, r*c)} }

func From[T Number](rows ...[]T) *Matrix[T] {
	m := New[T](len(rows), len(rows[0]))
	for i, row := range rows {
		copy(m.d[i*m.c:], row)
	}
	return m
}

func Identity[T Number](n int) *Matrix[T] {
	m := New[T](n, n)
	for i := 0; i < n; i++ {
		m.d[i*n+i] = 1
	}
	return m
}

func (m *Matrix[T]) At(i, j int) T     { return m.d[i*m.c+j] }
func (m *Matrix[T]) Set(i, j int, v T) { m.d[i*m.c+j] = v }

func (m *Matrix[T]) Add(o *Matrix[T]) (*Matrix[T], error) {
	if m.r != o.r || m.c != o.c {
		return nil, fmt.Errorf("add %dx%d + %dx%d: %w", m.r, m.c, o.r, o.c, ErrShape)
	}
	out := New[T](m.r, m.c)
	for i := range m.d {
		out.d[i] = m.d[i] + o.d[i]
	}
	return out, nil
}

func (m *Matrix[T]) Mul(o *Matrix[T]) (*Matrix[T], error) {
	if m.c != o.r {
		return nil, fmt.Errorf("mul %dx%d * %dx%d: %w", m.r, m.c, o.r, o.c, ErrShape)
	}
	out := New[T](m.r, o.c)
	for i := 0; i < m.r; i++ {
		for k := 0; k < m.c; k++ {
			a := m.At(i, k)
			for j := 0; j < o.c; j++ {
				out.d[i*o.c+j] += a * o.At(k, j)
			}
		}
	}
	return out, nil
}

func (m *Matrix[T]) Scale(s T) *Matrix[T] {
	out := New[T](m.r, m.c)
	for i, v := range m.d {
		out.d[i] = v * s
	}
	return out
}

func (m *Matrix[T]) Transpose() *Matrix[T] {
	out := New[T](m.c, m.r)
	for i := 0; i < m.r; i++ {
		for j := 0; j < m.c; j++ {
			out.Set(j, i, m.At(i, j))
		}
	}
	return out
}

func (m *Matrix[T]) Pow(n int) (*Matrix[T], error) {
	if m.r != m.c {
		return nil, ErrShape
	}
	result := Identity[T](m.r)
	base := m
	for n > 0 {
		if n&1 == 1 {
			result, _ = result.Mul(base)
		}
		base, _ = base.Mul(base)
		n >>= 1
	}
	return result, nil
}

func (m *Matrix[T]) Trace() (t T) {
	for i := 0; i < min(m.r, m.c); i++ {
		t += m.At(i, i)
	}
	return
}

func (m *Matrix[T]) Equal(o *Matrix[T]) bool {
	if m.r != o.r || m.c != o.c {
		return false
	}
	for i := range m.d {
		if m.d[i] != o.d[i] {
			return false
		}
	}
	return true
}

func (m *Matrix[T]) String() string {
	var sb strings.Builder
	for i := 0; i < m.r; i++ {
		sb.WriteString("[")
		for j := 0; j < m.c; j++ {
			if j > 0 {
				sb.WriteString(" ")
			}
			fmt.Fprintf(&sb, "%v", m.At(i, j))
		}
		sb.WriteString("]\n")
	}
	return sb.String()
}

func Map[T, U Number](m *Matrix[T], f func(T) U) *Matrix[U] {
	out := New[U](m.r, m.c)
	for i, v := range m.d {
		out.d[i] = f(v)
	}
	return out
}

func Det(m *Matrix[float64]) float64 {
	n := m.r
	a := make([]float64, len(m.d))
	copy(a, m.d)
	det := 1.0
	for col := 0; col < n; col++ {
		piv := col
		for r := col + 1; r < n; r++ {
			if math.Abs(a[r*n+col]) > math.Abs(a[piv*n+col]) {
				piv = r
			}
		}
		if a[piv*n+col] == 0 {
			return 0
		}
		if piv != col {
			for j := 0; j < n; j++ {
				a[col*n+j], a[piv*n+j] = a[piv*n+j], a[col*n+j]
			}
			det = -det
		}
		det *= a[col*n+col]
		for r := col + 1; r < n; r++ {
			f := a[r*n+col] / a[col*n+col]
			for j := col; j < n; j++ {
				a[r*n+j] -= f * a[col*n+j]
			}
		}
	}
	return det
}

func Inverse(m *Matrix[float64]) (*Matrix[float64], error) {
	n := m.r
	if m.r != m.c {
		return nil, ErrShape
	}
	a := New[float64](n, 2*n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			a.Set(i, j, m.At(i, j))
		}
		a.Set(i, n+i, 1)
	}
	for col := 0; col < n; col++ {
		piv := col
		for r := col + 1; r < n; r++ {
			if math.Abs(a.At(r, col)) > math.Abs(a.At(piv, col)) {
				piv = r
			}
		}
		if math.Abs(a.At(piv, col)) < 1e-12 {
			return nil, errors.New("singular matrix")
		}
		for j := 0; j < 2*n; j++ {
			x, y := a.At(col, j), a.At(piv, j)
			a.Set(col, j, y)
			a.Set(piv, j, x)
		}
		p := a.At(col, col)
		for j := 0; j < 2*n; j++ {
			a.Set(col, j, a.At(col, j)/p)
		}
		for r := 0; r < n; r++ {
			if r != col {
				f := a.At(r, col)
				for j := 0; j < 2*n; j++ {
					a.Set(r, j, a.At(r, j)-f*a.At(col, j))
				}
			}
		}
	}
	out := New[float64](n, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			out.Set(i, j, a.At(i, n+j))
		}
	}
	return out, nil
}

type Fixed int64

func main() {
	a := From([]int{1, 2, 3}, []int{4, 5, 6})
	b := From([]int{7, 8}, []int{9, 10}, []int{11, 12})
	p, err := a.Mul(b)
	fmt.Print(p, err, "\n")
	_, err = a.Mul(a)
	fmt.Println(err, errors.Is(err, ErrShape))
	_, err = a.Add(b)
	fmt.Println(err)
	fmt.Print(a.Transpose())
	fmt.Println(a.Trace(), p.Trace(), a.Scale(3).At(1, 2))

	fibm := From([]int64{1, 1}, []int64{1, 0})
	for _, n := range []int{1, 10, 50, 90} {
		r, _ := fibm.Pow(n)
		fmt.Println("fib", n, r.At(0, 1))
	}
	id := Identity[float64](3)
	m := From([]float64{2, -1, 0}, []float64{-1, 2, -1}, []float64{0, -1, 2})
	prod, _ := m.Mul(id)
	fmt.Println(prod.Equal(m), Det(m))
	inv, err := Inverse(m)
	fmt.Println(err)
	back, _ := m.Mul(inv)
	maxErr := 0.0
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			want := 0.0
			if i == j {
				want = 1
			}
			maxErr = math.Max(maxErr, math.Abs(back.At(i, j)-want))
		}
	}
	fmt.Println(maxErr < 1e-12)
	fmt.Printf("%.4f\n", inv.d)
	_, err = Inverse(From([]float64{1, 2}, []float64{2, 4}))
	fmt.Println(err, Det(From([]float64{1, 2}, []float64{2, 4})))

	f32 := From([]float32{0.5, 1.5}, []float32{2.5, 3.5})
	sq, _ := f32.Mul(f32)
	fmt.Print(sq)
	asInt := Map(sq, func(v float32) int { return int(v * 4) })
	fmt.Print(asInt)
	fx := From([]Fixed{1, 2}, []Fixed{3, 4})
	fx2, _ := fx.Pow(5)
	fmt.Print(fx2)

	n := 60
	big := New[float64](n, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			big.Set(i, j, float64((i*j)%7)+float64(i+j)/100)
		}
	}
	sq2, _ := big.Mul(big)
	t := sq2.Transpose().Transpose()
	fmt.Printf("%.3f %.3f %v\n", sq2.Trace(), sq2.At(n-1, n-1), t.Equal(sq2))
}
