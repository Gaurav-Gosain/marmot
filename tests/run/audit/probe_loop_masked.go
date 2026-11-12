// Edge cases for the loop optimizer (-par); the expected output is from gc.
// Products are rounded with float64() so gc on arm64 (which fuses a*b+c)
// and amd64 print the same.
package main

import "fmt"

const W, H = 600, 400

//go:noinline
func escape(out []int, maxIter int) {
	for py := 0; py < H; py++ {
		for px := 0; px < W; px++ {
			x0 := float64(float64(px)/W*3.5) - 2.5
			y0 := float64(float64(py)/H*2) - 1
			x, y := 0.0, 0.0
			i := 0
			for ; i < maxIter && float64(x*x)+float64(y*y) <= 4; i++ {
				x, y = float64(x*x)-float64(y*y)+x0, float64(2*x*y)+y0
			}
			out[py*W+px] = i
		}
	}
}

//go:noinline
func escapeStore(out []float64, trace []float64, maxIter int) {
	for px := 0; px < W; px++ {
		x0 := float64(float64(px)/W*3.5) - 2.5
		x, y := 0.0, 0.0
		i := 0
		for ; i < maxIter && float64(x*x)+float64(y*y) <= 4; i++ {
			x, y = float64(x*x)-float64(y*y)+x0, float64(2*x*y)+0.3
			trace[px] = x // store in the early exit loop
		}
		out[px] = x + float64(i)
	}
}

//go:noinline
func search(a []int, keys []int, res []int) {
	for k, key := range keys {
		j := 0
		for j < len(a) && a[j] != key {
			j++
		}
		res[k] = j
	}
}

//go:noinline
func interchange(a, b [][]float64, c [][]float64) {
	n := len(a)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			s := 0.0
			for k := 0; k < n; k++ {
				s += float64(a[i][k] * b[k][j])
			}
			c[i][j] = s
		}
	}
}

func main() {
	out := make([]int, W*H)
	escape(out, 200)
	t := 0
	for _, v := range out {
		t += v
	}
	fmt.Println("escape", t, out[0], out[W*H/2+W/2])
	o2 := make([]float64, W)
	tr := make([]float64, W)
	escapeStore(o2, tr, 100)
	s := 0.0
	for i := range o2 {
		s += o2[i] + tr[i]
	}
	fmt.Println("escapeStore", s)
	a := make([]int, 5000)
	for i := range a {
		a[i] = i * 3
	}
	keys := make([]int, 2000)
	for i := range keys {
		keys[i] = i * 7
	}
	res := make([]int, 2000)
	search(a, keys, res)
	fmt.Println("search", res[0], res[1], res[3], res[1999])

	n := 120
	A := make([][]float64, n)
	B := make([][]float64, n)
	C := make([][]float64, n)
	for i := 0; i < n; i++ {
		A[i] = make([]float64, n)
		B[i] = make([]float64, n)
		C[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			A[i][j] = float64((i*j)%7) * 1e15
			B[i][j] = 1 / float64(1+i+j)
			if j%3 == 0 {
				A[i][j] = 1e-3
			}
		}
	}
	interchange(A, B, C)
	cs := 0.0
	for i := range C {
		for _, v := range C[i] {
			cs += v
		}
	}
	fmt.Printf("interchange %.17g %.17g\n", cs, C[7][11])
	// Output aliases an input row.
	C2 := make([][]float64, n)
	copy(C2, A)
	interchange(A, B, C2)
	fmt.Printf("interchange alias %.17g\n", C2[3][5])
}
