package main

import "strconv"

func main() {
	for _, v := range []int64{0, 1, -1, 255, -255, 1 << 62, -1 << 63} {
		println(strconv.FormatInt(v, 10), strconv.FormatInt(v, 2), strconv.FormatInt(v, 16), strconv.FormatInt(v, 36))
	}
	println(strconv.FormatUint(1<<64-1, 10), strconv.FormatUint(1<<64-1, 16), strconv.FormatUint(1<<64-1, 2))
	println(strconv.Itoa(1<<31), strconv.Itoa(-1<<31))
	b := strconv.AppendInt(nil, -42, 10)
	b = strconv.AppendUint(b, 7, 2)
	b = strconv.AppendBool(b, true)
	b = strconv.AppendQuote(b, "q")
	println(string(b))
}
