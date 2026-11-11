package main

func v(s string, n int) int { println("eval", s); return n }

func main() {
	switch v("tag", 2) {
	case v("a", 1):
		println("1")
	case v("b", 2), v("c", 3):
		println("2")
	case v("d", 4):
		println("4")
	}
	switch {
	case v("x", 0) > 0:
	case v("y", 1) > 0:
		println("y")
	case v("z", 2) > 0:
	}
}
