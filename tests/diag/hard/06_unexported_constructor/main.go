package main

import (
	"fmt"

	"example.com/app/cache"
)

func main() {
	c := cache.newCache(2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	fmt.Println(c.Len())
}
