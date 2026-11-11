package main

import (
	"bufio"
	"os"
)

func main() {
	w := bufio.NewWriter(os.Stdout)
	w.WriteString("one ")
	w.WriteByte('2')
	w.WriteRune('é')
	w.Write([]byte(" four\n"))
	println(w.Buffered(), w.Available() > 0, w.Size())
	println(w.Flush() == nil, w.Buffered())
	small := bufio.NewWriterSize(os.Stdout, 16)
	for i := 0; i < 5; i++ {
		small.WriteString("0123456789")
	}
	println(small.Buffered())
	small.WriteString("\n")
	small.Flush()
	w.WriteString("unflushed is lost\n")
}
