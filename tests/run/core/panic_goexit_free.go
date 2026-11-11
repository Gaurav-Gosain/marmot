package main

func main() {
	defer func() {
		println(recover() != nil)
		defer func() {
			println(recover() != nil)
		}()
		panic("again")
	}()
	panic("one")
}
