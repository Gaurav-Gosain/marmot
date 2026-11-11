package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
)

func main() {
	var buf bytes.Buffer
	l := log.New(&buf, "app: ", 0)
	l.Println("hello", 42)
	l.Printf("value=%d", 7)
	l.SetPrefix("[x] ")
	l.Print("no newline")
	fmt.Print(buf.String())
	fmt.Println(l.Prefix(), l.Flags())
	l2 := log.New(os.Stdout, "", log.Lmsgprefix)
	l2.SetPrefix("P: ")
	l2.Println("msg")
	l3 := log.New(os.Stdout, "sf ", log.Lshortfile)
	l3.Println("with file")
	defer func() { fmt.Println("recovered:", recover()) }()
	log.New(os.Stdout, "", 0).Panicf("bad %d", 3)
}
