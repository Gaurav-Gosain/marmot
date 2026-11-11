package main

func main() {
	s := "immutable"
	t := s[2:6]
	b := []byte(s)
	b[2] = 'X'
	println(s, t, string(b))
	u := string(b[:4])
	b[0] = 'Q'
	println(u, string(b))
	bs := []byte("hello")
	s2 := string(bs)
	copy(bs, "J")
	println(s2, string(bs))
	n := copy(bs, "abcdefgh")
	println(n, string(bs))
	bs = append(bs[:2], "XYZ"...)
	println(string(bs))
}
