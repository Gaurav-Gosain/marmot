package main

import "fmt"

// T has a method, so it has a method table and a named type descriptor.
type T struct{ N int }

func (t T) String() string { return fmt.Sprintf("T(%d)", t.N) }

var inits int

func init() { inits++ }

func add(a, b int) int { return a + b }

func main() { fmt.Println(add(1, 2), T{3}, inits) }
