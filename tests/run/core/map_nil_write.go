package main

func main() {
	var m map[string]int
	println(m["a"], len(m))
	delete(m, "a")
	println("before")
	m["a"] = 1
	println("after")
}
