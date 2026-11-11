package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

func main() {
	dir, _ := os.MkdirTemp("", "marmot")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", p)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer ln.Close()
	fmt.Println(ln.Addr().Network())
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(c, c)
				c.Close()
			}()
		}
	}()
	for _, m := range []string{"one", "two two", ""} {
		c, err := net.Dial("unix", p)
		if err != nil {
			fmt.Println(err)
			return
		}
		c.Write([]byte(m))
		c.(*net.UnixConn).CloseWrite()
		b, _ := io.ReadAll(c)
		fmt.Printf("%q\n", b)
		c.Close()
	}
	_, err = net.Dial("unix", filepath.Join(dir, "missing"))
	fmt.Println(err != nil)
	a, b := net.Pipe()
	go func() {
		a.Write([]byte("through net.Pipe"))
		a.Close()
	}()
	all, _ := io.ReadAll(b)
	fmt.Println(string(all))
}
