package main

import "strconv"

func main() {
	x := uint64(88172645463325252)
	bad := 0
	for i := 0; i < 2000; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		v := int64(x)
		for _, base := range []int{2, 8, 10, 16, 36} {
			s := strconv.FormatInt(v, base)
			back, err := strconv.ParseInt(s, base, 64)
			if err != nil || back != v {
				bad++
			}
		}
		fv := float64(v) / 1e5
		fs := strconv.FormatFloat(fv, 'g', -1, 64)
		fb, _ := strconv.ParseFloat(fs, 64)
		if fb != fv {
			bad++
		}
	}
	println(bad)
}
