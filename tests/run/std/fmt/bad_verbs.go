package main

import "fmt"

func main() {
	f := func(a ...any) { fmt.Printf(a[0].(string), a[1:]...); fmt.Println() }
	f("%d", "str")
	f("%s", 5)
	f("%d")
	f("%d %d", 1)
	f("%d", 1, 2)
	f("%z", 1)
	f("%!", 1)
	f("%", 1)
	f("%.*d", 3, 5)
	f("%[2]d %[1]d", 1, 2)
	f("%[3]d", 1)
	f("%t", 1)
	f("%x", 1.5)
	f("%c", "s")
	f("%s", nil)
	f("%d", nil)
	f("%v", struct{ f func() }{})
}
