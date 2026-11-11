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

// Escape-time loop (mandelbrot's shape): masked jam runs 4 pixels per inner
// step; width 403 leaves a remainder of 3 for the original loop.
//go:noinline
func escape(out []int32, w, h int) int {
	inside := 0
	for py := 0; py < h; py++ {
		ci := 2.0*float64(py)/float64(h) - 1.0
		for px := 0; px < w; px++ {
			cr := 2.0*float64(px)/float64(w) - 1.5
			zr, zi := 0.0, 0.0
			i := 0
			for ; i < 60 && zr*zr+zi*zi <= 4.0; i++ {
				zr, zi = zr*zr-zi*zi+cr, 2*zr*zi+ci
			}
			if i == 60 {
				inside++
			}
			out[py*w+px] = int32(i)*1000 + int32(zr*100)
		}
	}
	return inside
}

// Strided writes (Rule P condition 5, strided form): column i of b over rows
// j, row stride 900. With w <= 900 the columns are disjoint (a run time check
// on w); with w = 950 columns 900..949 overlap the next row's first columns,
// the check fails and the serial loop runs.
//go:noinline
func transposeInto(b, a []int64, w int) {
	for i := 0; i < w; i++ {
		for j := 0; j < 700; j++ {
			b[j*900+i] = a[i*700+j] + int64(i)
		}
	}
}

// Byte count idiom (Rule D): replaced by rt_count_byte.
//go:noinline
func countEq(b []byte, c byte, start int) int {
	n := start
	for _, x := range b {
		if x == c {
			n++
		}
	}
	return n
}

//go:noinline
func countNe(b []byte, c byte) int {
	n := 0
	for i := 0; i < len(b); i++ {
		if b[i] != c {
			n++
		}
	}
	return n
}

// Not the idiom: the loop also writes.
//go:noinline
func countAndMark(b []byte, c byte) int {
	n := 0
	for i := range b {
		if b[i] == c {
			n++
			b[i] = c + 1
		}
	}
	return n
}

// Reduction arrays (Rule P condition 5, privatized): each chunk counts into
// a private zeroed copy, merged with atomic adds.
//go:noinline
func histo(b []byte) [256]int {
	var h [256]int
	for _, x := range b {
		h[x]++
	}
	return h
}

var ghist [64]int32

//go:noinline
func histoGlobal(b []byte) {
	for i := range b {
		ghist[b[i]&63] += int32(i & 3)
	}
}

// Not privatizable: the array is also read at another index in the loop.
//go:noinline
func histoPeek(b []byte) (int, [16]int) {
	var h [16]int
	peek := 0
	for _, x := range b {
		h[x&15]++
		peek += h[0]
	}
	return peek, h
}

// Narrow polynomial induction (verify's polyinduct16): a 16-bit second order
// recurrence over 2^20 iterations; the closed form must be exact past 2^16.
//go:noinline
func poly16(buf []int16) {
	var d, a int16
	for i := range buf {
		d += 1
		a += d
		buf[i] = a
	}
}

// Scans (Rule D): block sums, offsets, block rescans, tail.
//go:noinline
func scanAdd(a []int64) {
	for i := 1; i < len(a); i++ {
		a[i] += a[i-1]
	}
}

//go:noinline
func scanXor(dst, src []uint32, seed uint32) uint32 {
	r := seed
	for i := range src {
		r ^= src[i] * 2654435761
		dst[i] = r
	}
	return r
}

// Append loops (Rule V growth branches): with enough capacity the fast copy
// has no growslice; otherwise the original loop grows like Go.
//go:noinline
func appendN(s []int64, n int) []int64 {
	for i := 0; i < n; i++ {
		s = append(s, int64(i*3+1))
	}
	return s
}

// Values used after a parallel loop: the induction and values computed from
// it are recomputed for the last iteration.
//go:noinline
func lastIndexOf(a []int64) (int, int64) {
	i := 0
	for i = 0; i < len(a); i++ {
		a[i] = int64(i) * 5
	}
	return i, a[i-1] + int64(i)*7
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

	eo := make([]int32, 403*211)
	in := escape(eo, 403, 211)
	h := uint64(0)
	for _, v := range eo {
		h = h*1000003 + uint64(uint32(v))
	}
	println("escape", in, h, eo[0], eo[403*105+201])

	ta := make([]int64, 950*700)
	for i := range ta {
		ta[i] = int64(i*7%1000) - 300
	}
	tb := make([]int64, 700*900+100)
	transposeInto(tb, ta, 900)
	println("transpose", sum(tb), tb[5*900+7], tb[699*900+899])
	for i := range tb {
		tb[i] = 0
	}
	transposeInto(tb, ta, 950)
	bb := make([]byte, 100003)
	for i := range bb {
		bb[i] = byte(i*7 + i/13)
	}
	println("bytecount", countEq(bb, 3, 5), countEq(bb[17:99999], 200, 0), countNe(bb, 9), countEq(bb[:0], 1, 7))
	println("countmark", countAndMark(bb, 3), countEq(bb, 3, 0), countEq(bb, 4, 0))
	hb := make([]byte, 3000017)
	for i := range hb {
		hb[i] = byte(i*13 + i/7)
	}
	hh := histo(hb)
	println("histo", hh[0], hh[17], hh[255])
	histoGlobal(hb)
	histoGlobal(hb[5:])
	println("histoglobal", ghist[0], ghist[33], ghist[63])
	pk, ph := histoPeek(hb[:200000])
	println("histopeek", pk, ph[3])
	p16 := make([]int16, 1<<20)
	poly16(p16)
	println("poly16", p16[65535], p16[65536], p16[1<<19], p16[1<<20-1])
	scA := make([]int64, 1<<20+77)
	for i := range scA {
		scA[i] = int64(i%29) - 13
	}
	scanAdd(scA)
	println("scanadd", scA[0], scA[65535], scA[1<<19+5], scA[len(scA)-1])
	scB := scA[:5000]
	for i := range scB {
		scB[i] = int64(i % 7)
	}
	scanAdd(scB)
	println("scanadd-small", scB[4999])
	xs := make([]uint32, 1<<20+3)
	xd := make([]uint32, len(xs))
	for i := range xs {
		xs[i] = uint32(i * 7)
	}
	println("scanxor", scanXor(xd, xs, 99), xd[0], xd[70000], xd[len(xd)-1])
	ap := make([]int64, 10, 200000)
	shared := ap[:cap(ap)]
	ap = appendN(ap, 150000) // fits: writes the shared array
	println("append-fit", len(ap), sum(ap), shared[10], shared[150009])
	ap2 := appendN(ap, 100000) // grows: the first 50000 still go to shared
	println("append-grow", len(ap2), sum(ap2), shared[150010], shared[199999], len(appendN(nil, 777)))
	lp := make([]int64, 400001)
	li, lv := lastIndexOf(lp)
	println("lastindex", li, lv, sum(lp))
	println("transpose-overlap", sum(tb), tb[5*900+7], tb[699*900+949])
}
