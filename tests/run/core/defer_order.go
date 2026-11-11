package main

func main() {
	for i := 0; i < 4; i++ {
		defer println("defer", i)
	}
	defer func() {
		println("closure")
	}()
	println("body")
}
