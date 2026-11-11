package main

import (
	"bufio"
	"fmt"
	"io"
)

func main() {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for i := 0; i < 5; i++ {
			fmt.Fprintf(pw, "line %d\n", i)
		}
	}()
	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		println(sc.Text())
	}
	r, w := io.Pipe()
	go func() {
		w.CloseWithError(fmt.Errorf("producer failed"))
	}()
	_, err := io.ReadAll(r)
	println(err.Error())
}
