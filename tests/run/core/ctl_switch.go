package main

func classify(n int) string {
	switch {
	case n < 0:
		return "neg"
	case n == 0:
		return "zero"
	case n < 10:
		return "small"
	}
	return "big"
}

func name(n int) string {
	switch n {
	case 1:
		return "one"
	case 2, 3:
		return "two-three"
	default:
		return "many"
	case 4:
		return "four"
	}
}

func main() {
	println(classify(-1), classify(0), classify(5), classify(50))
	println(name(1), name(3), name(4), name(9))
	switch x := 5; x {
	case 5:
		println("five")
		fallthrough
	case 6:
		println("six")
		fallthrough
	case 7:
		println("seven")
	case 8:
		println("eight")
	}
	switch s := "go"; s {
	case "c":
		println("c")
	case "go", "rust":
		println("go-rust")
	}
	switch {
	default:
		println("default only")
	}
	switch f := func() int { println("eval"); return 2 }(); f {
	case 1:
	case 2:
		println("two")
	}
	switch 3.0 {
	case 3:
		println("float 3")
	}
	var i interface{} = 3
	switch i {
	case "3":
		println("str")
	case 3:
		println("int")
	}
}
