package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	items := []Item{{Name: "a", Price: 2, Qty: 3}, {Name: "b", Prise: 1.5, Qty: 2}}
	count := len(items)
	fmt.Println(Totl(items))
	fmt.Println(strings.ToUpper(items[0].name))
}
