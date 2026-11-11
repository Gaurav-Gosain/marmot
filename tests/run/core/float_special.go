package main

func main() {
	var z float64
	nan := z / z
	inf := 1 / z
	ninf := -1 / z
	println(nan, inf, ninf)
	println(nan == nan, nan != nan, nan < 1, nan > 1, nan <= nan, nan >= nan)
	println(inf > 1e308, ninf < -1e308, inf == inf, inf-inf)
	println(inf+1, inf*0, ninf*ninf)
	var negz float64 = -1
	negz *= z
	println(negz, negz == 0, 1/negz)
	var f32 float32
	println(f32/f32, 1/f32)
}
