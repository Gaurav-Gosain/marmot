package main

func main() {
	defer func() {
		r := recover()
		println(r != nil)
		_, isErr := r.(error)
		println(isErr)
	}()
	panic(nil)
}
