package main

func main() {
	s := "hello, world"
	println(len(s), s[0], s[7], s[len(s)-1])
	println(s[:5], s[7:], s[3:8], s[:0] == "", s[12:] == "")
	t := s + "!" + "!"
	println(t, len(t))
	println(s == "hello, world", s != t, s < t, s > "hello", "a" < "b", "" < "a")
	var e string
	println(len(e), e == "", e+e == "")
	u := "héllo"
	println(len(u), u[1], u[2])
	b := "abc"
	b += "def"
	b += b
	println(b)
	println("a\tb\\n\"q\"")
	println(`raw\n string`)
	println("\x41é\U0001F600\101")
}
