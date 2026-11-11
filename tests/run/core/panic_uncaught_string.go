package main

func main() {
	defer println("deferred runs")
	println("start")
	panic("fatal problem")
}
