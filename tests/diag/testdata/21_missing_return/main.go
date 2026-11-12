package main

func sign(x int) int {
	if x < 0 {
		return -1
	} else if x > 0 {
		return 1
	}
}

func main() { println(sign(1)) }
