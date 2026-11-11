package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

type upper struct{ w *strings.Builder }

func (u upper) Write(p []byte) (int, error) {
	return u.w.WriteString(strings.ToUpper(string(p)))
}

func main() {
	var b bytes.Buffer
	n, err := fmt.Fprint(&b, "a", 1, "b")
	println(n, err == nil, b.String())
	b.Reset()
	fmt.Fprintf(&b, "%03d|%s", 7, "x")
	fmt.Fprintln(&b, "end")
	println(b.String())
	var sb strings.Builder
	fmt.Fprintf(upper{&sb}, "hello %s", "world")
	println(sb.String())
	fmt.Fprintln(os.Stdout, "stdout line")
	n, _ = fmt.Fprintf(os.Stdout, "%s\n", "counted")
	println(n)
	fmt.Fprint(os.Stderr, "stderr text\n")
	println(fmt.Sprintf("%5.1f|%-5d|%5s", 2.25, 3, "r"))
	a := fmt.Append(nil, "x", 1)
	a = fmt.Appendf(a, "-%d", 2)
	a = fmt.Appendln(a, "end")
	println(string(a))
}
