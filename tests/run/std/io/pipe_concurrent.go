package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

func main() {
	pr, pw := io.Pipe()
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				fmt.Fprintf(pw, "w%d-%02d\n", w, i)
			}
		}()
	}
	go func() {
		wg.Wait()
		pw.Close()
	}()
	var lines []string
	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	sort.Strings(lines)
	fmt.Println(len(lines), lines[0], lines[len(lines)-1])
	r2, w2 := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		_, err := w2.Write([]byte(strings.Repeat("x", 100)))
		errc <- err
	}()
	buf := make([]byte, 10)
	n, _ := r2.Read(buf)
	r2.CloseWithError(fmt.Errorf("reader gave up"))
	fmt.Println(n, <-errc)
	r3, w3 := io.Pipe()
	go func() {
		w3.Write([]byte("abc"))
		w3.CloseWithError(io.ErrUnexpectedEOF)
	}()
	b, err := io.ReadAll(r3)
	fmt.Println(string(b), err)
}
