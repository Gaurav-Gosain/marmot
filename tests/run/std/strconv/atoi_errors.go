package main

import (
	"errors"
	"strconv"
)

func main() {
	for _, s := range []string{"", "+5", "-0", "007", " 1", "1 ", "9223372036854775807", "9223372036854775808", "-9223372036854775808", "-9223372036854775809", "1_000", "0x10", "١"} {
		n, err := strconv.Atoi(s)
		if err != nil {
			ne := err.(*strconv.NumError)
			println(ne.Func, ne.Num, ne.Err == strconv.ErrSyntax, ne.Err == strconv.ErrRange, n)
		} else {
			println(n)
		}
	}
	_, err := strconv.Atoi("x")
	println(errors.Is(err, strconv.ErrSyntax))
}
