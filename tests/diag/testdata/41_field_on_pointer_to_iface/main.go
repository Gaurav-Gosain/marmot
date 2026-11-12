package main

type R interface{ Read() int }

func f(r *R) int {
	return r.Read()
}

func main() { println(f(nil)) }
