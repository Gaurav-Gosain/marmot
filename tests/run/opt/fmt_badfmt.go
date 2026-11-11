package main

import "fmt"

// Malformed constant format strings. go vet rejects these on purpose, so this file is skipped
// by tests/vet.sh. The specializer must report errors exactly as fmt does at run time.
func main() {
	fmt.Printf("%t %v %p\n", true, nil, nil)
	fmt.Printf("%d %s\n", "wrong", 5)
	fmt.Printf("%d\n")
	fmt.Printf("%d %d\n", 1)
	fmt.Printf("extra\n", 1, "x")
	fmt.Printf("%!|%z|%\n", 1)
	fmt.Printf("%[3]d|%[0]d|%.*d\n", 1, 2)
	fmt.Println(fmt.Sprintf("%d%%%s", 5, "x"), fmt.Sprintf("%v", struct{ f func() }{}) != "")
}
