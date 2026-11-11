package main

import "strconv"

func main() {
	for _, s := range []string{"1", "-1.5", "+.5", "5.", "1e3", "1E-3", "0x1p4", "inf", "-Infinity", "nan", "1_0.5", "", ".", "1e", "1.5.5", "  1", "1e400", "-1e400", "4.9e-324", "2.4e-324"} {
		f, err := strconv.ParseFloat(s, 64)
		println(s, f, err != nil)
	}
	f, err := strconv.ParseFloat("1.1", 32)
	println(f, err == nil, float32(f) == 1.1)
	_, err = strconv.ParseFloat("1e39", 32)
	println(err != nil)
}
