package main

func main() {
	defer func() {
		r := recover()
		println("outer recovered", r.(string))
	}()
	defer func() {
		r := recover()
		println("inner recovered", r.(string))
		panic("second")
	}()
	panic("first")
}
