package main

func main() {
	s := "héllo, 世界"
	b := []byte(s)
	println(len(b), b[1], b[2])
	r := []rune(s)
	println(len(r), r[1], r[7])
	println(string(b) == s, string(r) == s)
	println(string(rune(65)), string(rune(0x4e16)), string(rune(-1)), string(rune(0x10ffff+1)))
	println(string(rune(0xd800)))
	b[0] = 'J'
	println(s[0], string(b))
	var nb []byte
	println(string(nb) == "", len([]byte("")))
	bad := string([]byte{0xff, 'a', 0xc3})
	println(len([]rune(bad)))
	for _, c := range []rune(bad) {
		println(c)
	}
	var ru rune = 'x'
	println(string(ru))
	println(string([]rune{72, 105, 0x1F600}))
}
