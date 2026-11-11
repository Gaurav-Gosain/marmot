package main

import "strconv"

func main() {
	vals := []float64{0, 1, -1, 3.14159265358979, 1e6, 1e21, 1e-6, 1e-7, 123456789, 0.000123, 100, 2.5, 1.0 / 3}
	for _, v := range vals {
		println(strconv.FormatFloat(v, 'f', -1, 64), strconv.FormatFloat(v, 'e', -1, 64), strconv.FormatFloat(v, 'g', -1, 64))
	}
	for _, p := range []int{0, 1, 3, 10} {
		println(strconv.FormatFloat(3.14159265358979, 'f', p, 64), strconv.FormatFloat(3.14159265358979, 'e', p, 64), strconv.FormatFloat(3.14159265358979, 'g', p, 64))
	}
	println(strconv.FormatFloat(2.5, 'f', 0, 64), strconv.FormatFloat(3.5, 'f', 0, 64), strconv.FormatFloat(0.125, 'f', 2, 64))
	println(strconv.FormatFloat(255, 'x', -1, 64), strconv.FormatFloat(1.5, 'b', -1, 64))
}
