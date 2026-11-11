package main

func helper() any { return recover() }

func main() {
	defer func() {
		r := recover()
		println("outer", r.(string))
	}()
	defer func() {
		println("helper got", helper() == nil)
	}()
	panic("p")
}
