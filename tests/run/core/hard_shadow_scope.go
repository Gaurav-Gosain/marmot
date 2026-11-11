package main

var x = "global"

func show() string { return x }

type T int

func main() {
	println(show())
	x := "local"
	println(x, show())
	{
		x := 5
		x++
		println(x)
	}
	if x := 1; x > 0 {
		x := "inner"
		println(x)
	}
	for x := 0; x < 1; x++ {
		x := x + 10
		println(x)
	}
	T := 3
	println(T)
	len := func(s string) int { return 42 }
	println(len("abc"))
	true := false
	println(true)
	var nil_ = 0
	println(nil_)
	switch x := interface{}(x).(type) {
	case string:
		println("s", x)
	}
}
