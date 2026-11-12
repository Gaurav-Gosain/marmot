package main

import "fmt"

func echo(rw ReadWriter) string {
	rw.Write("hi")
	return rw.Read()
}

func main() {
	fmt.Println(echo(&Buf{}))
}
