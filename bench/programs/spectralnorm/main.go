// spectral-norm: float loops over a virtual matrix.
package main

const n = 11000

func sqrt(x float64) float64 {
	z := x / 2
	for i := 0; i < 40; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}

func a(i, j int) float64 {
	return 1.0 / float64((i+j)*(i+j+1)/2+i+1)
}

func mulAv(v, av []float64) {
	for i := range av {
		s := 0.0
		for j := range v {
			s += a(i, j) * v[j]
		}
		av[i] = s
	}
}

func mulAtv(v, atv []float64) {
	for i := range atv {
		s := 0.0
		for j := range v {
			s += a(j, i) * v[j]
		}
		atv[i] = s
	}
}

func mulAtAv(v, out, tmp []float64) {
	mulAv(v, tmp)
	mulAtv(tmp, out)
}

func main() {
	u := make([]float64, n)
	v := make([]float64, n)
	tmp := make([]float64, n)
	for i := range u {
		u[i] = 1
	}
	for i := 0; i < 10; i++ {
		mulAtAv(u, v, tmp)
		mulAtAv(v, u, tmp)
	}
	var vbv, vv float64
	for i := 0; i < n; i++ {
		vbv += u[i] * v[i]
		vv += v[i] * v[i]
	}
	println("spectralnorm", int64(sqrt(vbv/vv)*1e9))
}
