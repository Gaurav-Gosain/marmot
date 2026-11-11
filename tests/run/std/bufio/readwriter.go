package main

import (
	"bufio"
	"bytes"
	"strings"
)

func main() {
	var out bytes.Buffer
	rw := bufio.NewReadWriter(bufio.NewReader(strings.NewReader("input line\n")), bufio.NewWriter(&out))
	line, _ := rw.ReadString('\n')
	rw.WriteString("echo: " + line)
	println(out.Len())
	rw.Flush()
	println(out.String())
}
