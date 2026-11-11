// par.go: loops that loop-parallelize transforms, and loops it must not.
// Output must equal stock Go's (par.expected).
package main

import "math"

const n = 1 << 20

//go:noinline
func fill(a []int64, seed int64) {
	x := seed
	for i := range a {
		x = x*6364136223846793005 + 1442695040888963407
		a[i] = x >> 11
	}
}

//go:noinline
func sum(a []int64) int64 {
	s := int64(0)
	for i := range a {
		s += a[i] * 3
	}
	return s
}

//go:noinline
func xorAll(a []int64) int64 {
	s := int64(-1)
	for i := 0; i < len(a); i++ {
		s ^= a[i]
	}
	return s
}

//go:noinline
func minMax(a []int64) (int64, int64) {
	lo, hi := a[0], a[0]
	for _, v := range a {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi
}

//go:noinline
func countIf(a []int64) int {
	c := 0
	for _, v := range a {
		if v&7 == 3 {
			c++
		}
	}
	return c
}

// Wrapping arithmetic: the partial sums overflow.
//go:noinline
func wrapSum(a []int64) int64 {
	s := int64(0x7fffffffffffff00)
	for i := range a {
		s += a[i] << 20
	}
	return s
}

// Independent element writes; dst and src may overlap at run time.
//go:noinline
func scale(dst, src []int64, k int64) {
	for i := range dst {
		if i < len(src) {
			dst[i] = src[i] * k
		}
	}
}

//go:noinline
func axpy(dst, x []float64, a float64) {
	x = x[:len(dst)]
	for i := range dst {
		dst[i] = a*x[i] + dst[i]
	}
}

// A float reduction keeps its order: not parallel without -ffast-fp.
//go:noinline
func fsum(a []float64) float64 {
	s := 0.0
	for _, v := range a {
		s += v
	}
	return s
}

// Loop carried dependence: must stay serial.
//go:noinline
func prefix(a []int64) {
	for i := 1; i < len(a); i++ {
		a[i] += a[i-1]
	}
}

// Rows of a 2D grid, each row independent.
//go:noinline
func rows(g []int32, w int) int64 {
	t := int64(0)
	for r := 0; r < len(g)/w; r++ {
		row := g[r*w : r*w+w]
		for c := range row {
			row[c] = int32(r*31 + c*7)
			t += int64(row[c])
		}
	}
	return t
}

// Early exit: not a counted loop.
//go:noinline
func find(a []int64, v int64) int {
	for i := range a {
		if a[i] == v {
			return i
		}
	}
	return -1
}

// Matrix-vector product over a virtual matrix (spectralnorm's shape): the
// i loop is parallel and jammed, (i+j)*(i+j+1)/2 is a quadratic induction,
// and each row's float sum keeps its order.
func av(i, j int) float64 {
	return 1.0 / float64((i+j)*(i+j+1)/2+i+1)
}

//go:noinline
func mulAv(v, out []float64) {
	for i := range out {
		s := 0.0
		for j := range v {
			s += av(i, j) * v[j]
		}
		out[i] = s
	}
}

//go:noinline
func mulAtv(v, out []float64) {
	for i := range out {
		s := 0.0
		for j := range v {
			s += av(j, i) * v[j]
		}
		out[i] = s
	}
}

// Bounds checks that go-bce cannot remove (n is not tied to len(a)):
// versioned, then parallel. With n > len(a) the slow copy must panic at the
// same index after writing the same elements.
//go:noinline
func fillN(a []int64, n int) {
	for i := 0; i < n; i++ {
		a[i] = int64(i*3) ^ 0x55
	}
}

func tryFill(a []int64, n int) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = r.(error).Error()
		}
	}()
	fillN(a, n)
	return "ok"
}

// Row lengths are loaded inside the nest: versioned with a check loop over
// len(m[i]); a short row must panic at the same place as Go.
//go:noinline
func rowSums(m [][]int64, cols int) int64 {
	t := int64(0)
	for i := range m {
		for j := 0; j < cols; j++ {
			t += m[i][j] ^ int64(j)
		}
	}
	return t
}

func tryRows(m [][]int64, cols int) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = r.(error).Error()
		}
	}()
	return "ok " + itoa(rowSums(m, cols))
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	var b [24]byte
	i := len(b)
	for v != 0 {
		d := v % 10
		if d < 0 {
			d = -d
		}
		i--
		b[i] = byte('0' + d)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Matrix multiply over [][]float64: bounds checks versioned with row length
// check loops, rows checked disjoint at run time, the i loop parallel, the
// j/k nest interchanged so j vectorizes. Results must be bit identical (the
// sum is fused on arm64 like gc does).
//go:noinline
func matmulRows(a, b, c [][]float64) {
	for i := range c {
		for j := range c[i] {
			s := 0.0
			for k := range b {
				s += a[i][k] * b[k][j]
			}
			c[i][j] = s
		}
	}
}

func matrix(r, c int, seed float64) [][]float64 {
	m := make([][]float64, r)
	for i := range m {
		m[i] = make([]float64, c)
		for j := range m[i] {
			m[i][j] = math.Sin(seed+float64(i*c+j)) * 1.0000001
		}
	}
	return m
}

func bits(m [][]float64) uint64 {
	h := uint64(0)
	for i := range m {
		for j := range m[i] {
			h = h*31 + math.Float64bits(m[i][j])
		}
	}
	return h
}

// The bound of a versioned loop comes from a data dependent loop before it
// (the probe7 shape without unsafe): the fast copy must run all n iterations.
//go:noinline
func untilZero(a []int64) []int64 {
	n := 0
	for a[n] != 0 {
		n++
	}
	e := make([]int64, n)
	for i := 0; i < n; i++ {
		e[i] = a[i]*2 + int64(len(itoa(a[i])))
	}
	return e
}

func main() {
	a := make([]int64, n)
	fill(a, 42)
	println("sum", sum(a))
	println("xor", xorAll(a))
	lo, hi := minMax(a)
	println("minmax", lo, hi)
	println("count", countIf(a))
	println("wrap", wrapSum(a))

	b := make([]int64, n)
	scale(b, a, 5)
	println("scale", sum(b))
	// Overlapping: dst starts 1 element after src. The serial result
	// smears a[0]; a parallel run without the alias check would differ.
	c := make([]int64, n+1)
	copy(c, a)
	scale(c[1:], c[:n], 1)
	println("overlap", sum(c), c[n], c[n/2])
	scale(c[:n], c[1:], 1)
	println("overlap2", sum(c), c[0], c[n/2])

	f := make([]float64, n)
	x := make([]float64, n)
	for i := range f {
		f[i] = float64(i) * 0.25
		x[i] = float64(n-i) * 0.5
	}
	axpy(f, x, 1.5)
	println("axpy", f[0], f[n/3], f[n-1])
	println("fsum", fsum(f))

	p := make([]int64, n)
	for i := range p {
		p[i] = int64(i & 15)
	}
	prefix(p)
	println("prefix", p[n-1], p[n/2])

	g := make([]int32, 1000*1000)
	println("rows", rows(g, 1000), g[999*1000+999])
	println("find", find(a, a[n-7]), find(a, 12345))

	u := make([]float64, 1001)
	w := make([]float64, 1001)
	for i := range u {
		u[i] = 1 + float64(i%7)*0.125
	}
	for r := 0; r < 3; r++ {
		mulAv(u, w)
		mulAtv(w, u)
	}
	println("mv", u[0], u[500], u[1000], w[3])

	fa := make([]int64, 300000)
	println("fill", tryFill(fa, len(fa)), sum(fa))
	for i := range fa {
		fa[i] = 0
	}
	println("fill2", tryFill(fa, len(fa)+5), sum(fa), fa[len(fa)-1])
	for i := range fa {
		fa[i] = 0
	}
	println("fill3", tryFill(fa[:1000], 400000), sum(fa), fa[999], fa[1000])

	m := make([][]int64, 600)
	for i := range m {
		m[i] = make([]int64, 700+i%3)
		for j := range m[i] {
			m[i][j] = int64(i*j) - 5
		}
	}
	println("rows1", tryRows(m, 700))
	m[417] = m[417][:650]
	println("rows2", tryRows(m, 700))

	ma := matrix(150, 130, 1)
	mb := matrix(130, 170, 2)
	mc := matrix(150, 170, 3)
	matmulRows(ma, mb, mc)
	println("matmul", bits(mc))
	// c aliases b: the row check fails and the serial loop runs.
	sq := matrix(120, 120, 4)
	sa := matrix(120, 120, 5)
	matmulRows(sa, sq, sq)
	println("matmul-alias", bits(sq))

	z := make([]int64, 5000)
	for i := 0; i < 4321; i++ {
		z[i] = int64(i*7 + 1)
	}
	ez := untilZero(z)
	println("untilzero", len(ez), ez[0], ez[1], ez[4320], sum(ez))
}
