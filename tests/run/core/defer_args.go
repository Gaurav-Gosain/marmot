package main

func show(tag string, v int) int {
	println(tag, v)
	return v
}

type T struct{ n int }

func (t T) Print()   { println("val", t.n) }
func (t *T) PPrint() { println("ptr", t.n) }

func main() {
	x := 1
	defer show("arg eval", x)
	defer func() { println("closure sees", x) }()
	x = 2
	t := T{1}
	defer t.Print()
	defer t.PPrint()
	t.n = 5
	defer show("nested", show("inner", 7))
	var f func()
	f = func() { println("f1") }
	defer f()
	f = func() { println("f2") }
	println("end of main")
}
