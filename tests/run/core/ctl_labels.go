package main

func main() {
outer:
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			if j == 2 {
				continue outer
			}
			if i == 3 {
				break outer
			}
			println(i, j)
		}
	}
	println("after outer")
loop:
	for i := 0; ; i++ {
		switch {
		case i == 2:
			continue loop
		case i > 4:
			break loop
		}
		println("i", i)
	}
sel:
	switch 1 {
	case 1:
		for i := 0; i < 3; i++ {
			if i == 1 {
				break sel
			}
		}
		println("unreachable")
	}
	println("done")
}
