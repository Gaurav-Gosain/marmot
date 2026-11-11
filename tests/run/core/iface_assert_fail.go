package main

func main() {
	var i interface{} = "hello"
	println("start")
	n := i.(int)
	println(n)
}
