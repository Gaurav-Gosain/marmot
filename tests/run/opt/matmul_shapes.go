package main

func matmul(c, a, b []float64, n, m, p int) {
	for i := 0; i < n; i++ {
		for j := 0; j < p; j++ {
			s := 0.0
			for k := 0; k < m; k++ {
				s += a[i*m+k] * b[k*p+j]
			}
			c[i*p+j] = s
		}
	}
}

func matmulAcc(c, a, b []int, n int) {
	for i := 0; i < n; i++ {
		for k := 0; k < n; k++ {
			for j := 0; j < n; j++ {
				c[i*n+j] += a[i*n+k] * b[k*n+j]
			}
		}
	}
}

func main() {
	for _, d := range [][3]int{{1, 1, 1}, {2, 3, 4}, {5, 5, 5}, {7, 1, 9}, {16, 16, 16}, {17, 13, 19}, {33, 31, 35}} {
		n, m, p := d[0], d[1], d[2]
		a := make([]float64, n*m)
		b := make([]float64, m*p)
		for i := range a {
			a[i] = float64(i%7) + 0.5
		}
		for i := range b {
			b[i] = float64(i%5) - 1.25
		}
		c := make([]float64, n*p)
		matmul(c, a, b, n, m, p)
		t := 0.0
		for _, v := range c {
			t += v
		}
		println(n, m, p, t, c[0], c[len(c)-1])
	}
	n := 24
	a := make([]int, n*n)
	b := make([]int, n*n)
	c := make([]int, n*n)
	for i := range a {
		a[i], b[i] = i%9, i%4-1
	}
	matmulAcc(c, a, b, n)
	matmulAcc(c, c, b, n)
	println(c[0], c[n*n-1], c[n*3+5])
	defer func() { println(recover().(error).Error()) }()
	matmul(make([]float64, 4), make([]float64, 4), make([]float64, 3), 2, 2, 2)
}
