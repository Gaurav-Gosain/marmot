package main

type E struct{}

func (E) Error() string { return "custom error value" }

func main() {
	panic(E{})
}
