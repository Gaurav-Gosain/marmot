package main

func a() {
	defer func() {
		r := recover()
		println("a recovered", r.(string))
	}()
	b()
}

func b() {
	defer func() {
		println("b defer 1")
	}()
	defer func() {
		panic("from b defer")
	}()
	defer func() {
		println("b defer 3")
	}()
	panic("original")
}

func main() {
	a()
	println("main continues")
	func() {
		defer func() { println("rec", recover().(string)) }()
		defer func() { recover(); panic("second") }()
		panic("first")
	}()
}
