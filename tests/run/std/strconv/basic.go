package main

import "strconv"

func main() {
	println(strconv.Itoa(-12345), strconv.Itoa(0))
	n, err := strconv.Atoi("987")
	println(n, err == nil)
	_, err = strconv.Atoi("12x")
	println(err.Error())
	_, err = strconv.Atoi("")
	println(err.Error())
	i, err := strconv.ParseInt("-ff", 16, 64)
	println(i, err == nil)
	_, err = strconv.ParseInt("300", 10, 8)
	println(err.Error())
	u, _ := strconv.ParseUint("18446744073709551615", 10, 64)
	println(u)
	b, _ := strconv.ParseBool("true")
	println(b)
	f, _ := strconv.ParseFloat("3.25", 64)
	println(f)
	println(strconv.FormatInt(-255, 2), strconv.FormatInt(255, 16), strconv.FormatInt(35, 36))
	println(strconv.Quote("hi\n\"x\"\t世"), strconv.QuoteToASCII("世"))
	s, err := strconv.Unquote(`"a\tb"`)
	println(s, err == nil)
	println(strconv.FormatFloat(1.5, 'f', 2, 64), strconv.FormatFloat(1234.5678, 'e', 3, 64), strconv.FormatFloat(0.1, 'g', -1, 64))
	println(strconv.FormatBool(false), string(strconv.AppendInt([]byte("n="), 42, 10)))
}
