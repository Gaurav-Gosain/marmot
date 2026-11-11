package main

type T struct {
	a int
	b [1000]int
}

func (t *T) M() int { return t.a }

func try(f func()) {
	defer func() { println(recover().(error).Error()) }()
	f()
}

func main() {
	var p *T
	try(func() { println(p.a) })
	try(func() { println(p.b[500]) })
	try(func() { println(p.M()) })
	try(func() { p.a = 1 })
	var pi *int
	try(func() { *pi = 3 })
	var ps *[]int
	try(func() { _ = len(*ps) })
	var f func(int) int
	try(func() { f(1) })
}
