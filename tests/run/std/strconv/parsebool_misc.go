package main

import "strconv"

func main() {
	for _, s := range []string{"1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE", "false", "False", "yes", "", "tRUE"} {
		b, err := strconv.ParseBool(s)
		println(s, b, err != nil)
	}
	println(strconv.IsPrint('a'), strconv.IsPrint('\n'), strconv.IsPrint('世'), strconv.CanBackquote("a`b"), strconv.CanBackquote("ab"))
	println(strconv.QuotedPrefix(`"abc"def`))
	println(strconv.IntSize)
}
