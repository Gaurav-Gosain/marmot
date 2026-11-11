package main

func level3() {
	defer println("l3 defer")
	panic("deep")
}

func level2() {
	defer func() {
		println("l2 defer")
	}()
	level3()
	println("not reached")
}

func level1() (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			println("recovered", r.(string))
			ok = true
		}
	}()
	level2()
	return false
}

func main() {
	println(level1())
	println("done")
}
