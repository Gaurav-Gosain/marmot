package main

type Namer interface{ Name() string }

type Dog struct{}

func (Dog) Name() []byte { return []byte("dog") }

func main() {
	var n Namer = Dog{}
	println(n.Name())
}
