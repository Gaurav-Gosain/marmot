package main

func main() {
	for i := 0; i < 3; i++ {
		println(i)
	}
	n := 0
	for n < 5 {
		n += 2
	}
	println(n)
	for {
		n--
		if n < 3 {
			break
		}
	}
	println(n)
	for i, j := 0, 10; i < j; i, j = i+1, j-1 {
		n += i * j
	}
	println(n)
	for i := range 4 {
		if i == 2 {
			continue
		}
		println("r", i)
	}
	var u uint8
	for u = 250; u >= 250; u++ {
	}
	println(u)
	for i := range int8(3) {
		println(i)
	}
	for range 2 {
		println("x")
	}
	k := 0
	for range k {
		println("never")
	}
}
