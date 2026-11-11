package main

type I interface{ Get() int }
type T struct{ n int }

func (t T) Get() int { return t.n }

func main() {
	f := I.Get
	println(f(T{3}))
	var i I = T{9}
	g := i.Get
	i = T{10}
	println(g(), i.Get())
}
