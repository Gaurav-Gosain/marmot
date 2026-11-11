package main

func seq(yield func(int) bool) {
	defer println("seq cleanup")
	yield(1)
	yield(2)
}

func main() {
	defer func() { println("recovered", recover().(string)) }()
	for i := range seq {
		println(i)
		if i == 2 {
			panic("in body")
		}
	}
}
