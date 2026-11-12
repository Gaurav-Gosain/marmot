package main

import (
	"fmt"

	"example.com/app/status"
)

func main() {
	s := status.Activ
	if s == status.Suspend {
		fmt.Println("no")
	}
	fmt.Println(s)
}
