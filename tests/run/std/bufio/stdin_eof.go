package main

import (
	"bufio"
	"os"
)

func main() {
	sc := bufio.NewScanner(os.Stdin)
	n := 0
	for sc.Scan() {
		n++
	}
	println(n, sc.Err() == nil)
	r := bufio.NewReader(os.Stdin)
	_, err := r.ReadString('\n')
	println(err != nil)
}
