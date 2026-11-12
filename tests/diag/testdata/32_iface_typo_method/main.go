package main

import "fmt"

type Temp float64

func (t Temp) Strng() string { return fmt.Sprintf("%.1f C", float64(t)) }

func main() {
	var s fmt.Stringer = Temp(1)
	fmt.Println(s)
}
