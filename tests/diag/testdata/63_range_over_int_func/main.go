package main

func count() int { return 3 }

func main() {
	for i := range count {
		println(i)
	}
}
