package main

type S struct{ a, b int }

func main() {
	s := S{1, 2}
	f := func() { s.a += 10 }
	f()
	println(s.a)
	arr := [3]int{1, 2, 3}
	g := func() { arr[0] = 99 }
	g()
	println(arr[0])
	str := "x"
	h := func() { str += "y" }
	h()
	h()
	println(str)
	var iface interface{} = 1
	k := func() { iface = "now string" }
	k()
	println(iface.(string))
	n := 1
	a := func() int { return n }
	n = 2
	println(a())
	b := func() func() int {
		m := n
		return func() int { m++; return m + n }
	}()
	println(b(), b())
	var big [100]int
	big[50] = 5
	c := func() int { return big[50] }
	big[50] = 6
	println(c())
}
