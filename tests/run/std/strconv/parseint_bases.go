package main

import "strconv"

func main() {
	for _, c := range []struct {
		s    string
		base int
		bits int
	}{{"ff", 16, 64}, {"0xff", 0, 64}, {"0b101", 0, 64}, {"0o17", 0, 64}, {"017", 0, 64}, {"1_000", 0, 64}, {"zz", 36, 64}, {"-128", 10, 8}, {"128", 10, 8}, {"-129", 10, 8}, {"12", 2, 64}, {"7", 1, 64}} {
		v, err := strconv.ParseInt(c.s, c.base, c.bits)
		println(c.s, v, err != nil)
	}
	u, err := strconv.ParseUint("256", 10, 8)
	println(u, err != nil)
	u, err = strconv.ParseUint("-1", 10, 64)
	println(u, err != nil)
}
