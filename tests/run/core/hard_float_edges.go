package main

func main() {
	var z float64
	nan := z / z
	inf := 1 / z
	println(nan == nan, nan != nan, nan < inf, nan > -inf)
	m := map[float64]int{}
	m[nan] = 1
	m[nan] = 2
	m[inf] = 3
	m[-z] = 4
	m[z] = 5
	println(len(m), m[inf], m[0])
	var f32 float32 = 0.1
	var f64 float64 = float64(f32)
	println(f64 == 0.1, f32 == 0.1, float32(f64) == f32)
	println(inf+inf, inf-inf == inf-inf, -inf < 0)
	println(int64(float64(1<<53)+1) == 1<<53)
	println(5.0/2, 5/2.0, float32(5)/2)
	x := 1e308
	println(x*10, -x*10, x*10 == inf)
	tiny := 5e-324
	println(tiny/2, tiny/2 == 0, tiny > 0)
	var h float32 = 16777216
	println(h+1 == h, h+2 == h)
	var s1, s2 float64 = 0.1, 0.2
	println(s1+s2 == 0.3, s1+s2)
}
