package main

import "strconv"

func main() {
	vals := []float64{0, 1, -1, 0.1, 1.0 / 3, 1e21, 1e-7, 123456789.125, 5e-324, 1.7976931348623157e308, 2.5, 100}
	for _, v := range vals {
		s := strconv.FormatFloat(v, 'g', -1, 64)
		back, _ := strconv.ParseFloat(s, 64)
		println(s, back == v)
	}
	f32 := []float32{0.1, 16777216, 3.4028235e38, 1e-45}
	for _, v := range f32 {
		s := strconv.FormatFloat(float64(v), 'g', -1, 32)
		back, _ := strconv.ParseFloat(s, 32)
		println(s, float32(back) == v)
	}
	_, err := strconv.ParseFloat("1e999", 64)
	println(err.Error())
	nan, _ := strconv.ParseFloat("NaN", 64)
	inf, _ := strconv.ParseFloat("-Inf", 64)
	println(nan != nan, inf < 0)
}
