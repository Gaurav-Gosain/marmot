package main

import "fmt"

func main() {
	fmt.Printf("[%*d] [%-*d] [%.*f]\n", 6, 42, 6, 42, 2, 3.14159)
	fmt.Printf("[%6v] [%-6v] [%06v]\n", 42, 42, 42)
	fmt.Printf("[%6v] [%-6v]\n", "ab", "ab")
	fmt.Printf("[%6v] [%-6v] [%6v]\n", true, true, nil)
	fmt.Printf("[%8v] [%-8v]\n", []int{1, 2}, []int{1, 2})
	fmt.Printf("[%5.1f%%] [%+d] [%+.2e]\n", 99.5, 0, 12345.678)
	fmt.Printf("[% d] [% d] [%+d]\n", 5, -5, 5)
	fmt.Printf("[%#v] [%#x] [%#o] [%#b]\n", "q", 255, 8, 5)
	fmt.Printf("[%x] [% X] [%#x]\n", "abc", "abc", "abc")
	fmt.Printf("[%10.3v] [%-10.3v]\n", 3.14159, "abcdef")
}
