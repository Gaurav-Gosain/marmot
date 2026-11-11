package main

type Animal interface{ Sound() string }

type Base struct{ name string }

func (b *Base) Sound() string { return b.name + "?" }

type Dog struct{ *Base }

func (d Dog) Sound() string { return d.Base.Sound() + "woof" }

type Cat struct{ Base }

type Loud struct{ Animal }

func (l Loud) Sound() string { return l.Animal.Sound() + "!!!" }

func main() {
	d := Dog{&Base{"rex"}}
	var a Animal = d
	println(a.Sound())
	c := &Cat{Base{"tom"}}
	a = c
	println(a.Sound())
	l := Loud{a}
	a = l
	println(a.Sound())
	a = Loud{Dog{&Base{"x"}}}
	println(a.Sound())
	_, isCat := a.(*Cat)
	_, isLoud := a.(Loud)
	println(isCat, isLoud)
	var e struct{ Animal }
	defer func() { println(recover() != nil) }()
	e.Sound()
}
