package main

func main() {
	var s []int
	for i := 0; i < 20; i++ {
		s = append(s, i)
		if cap(s) < len(s) {
			println("bad cap")
		}
	}
	println(len(s), s[19])
	s = append(s, s...)
	println(len(s), s[39])
	s = append(s[:5], s[10:]...)
	println(len(s), s[5], s[len(s)-1])
	var t []int
	t = append(t, t...)
	println(t == nil)
	t = append(t, []int{}...)
	println(t == nil)
	b := append([]byte("ab"), "cd"...)
	println(string(b))
	x := append([]string(nil), "a", "b", "c")
	println(len(x), x[2])
}
