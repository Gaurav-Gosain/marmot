package main

func main() {
	s := ""
	for i := 0; i < 20; i++ {
		s += string(rune('a' + i))
	}
	println(s)
	a, b, c := "x", "yy", "zzz"
	println(a+b+c, len(a+b+c))
	parts := []string{"one", "two", "three"}
	r := ""
	for i, p := range parts {
		if i > 0 {
			r += ","
		}
		r += p
	}
	println(r)
	big := ""
	for i := 0; i < 1000; i++ {
		big += "ab"
	}
	println(len(big), big[1998:])
	var e1, e2 string
	println(len(e1+e2), len(e1+"q"))
}
