package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen:", err)
		return
	}
	defer ln.Close()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	fmt.Println(host, port != "0" && port != "", ln.Addr().Network())
	var wg sync.WaitGroup
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					fmt.Fprintf(c, "echo:%s\n", sc.Text())
				}
			}()
		}
	}()
	for i := 0; i < 3; i++ {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			fmt.Println("dial:", err)
			return
		}
		r := bufio.NewReader(c)
		for _, m := range []string{"hello", "world", fmt.Sprint("n", i)} {
			fmt.Fprintln(c, m)
			l, _ := r.ReadString('\n')
			fmt.Print(l)
		}
		fmt.Println(c.RemoteAddr().Network(), c.LocalAddr().(*net.TCPAddr).IP)
		c.(*net.TCPConn).CloseWrite()
		rest, err := io.ReadAll(r)
		fmt.Println(len(rest), err)
		c.Close()
	}
	big := make([]byte, 1<<20)
	for i := range big {
		big[i] = byte(i * 7)
	}
	c, _ := net.Dial("tcp", ln.Addr().String())
	go func() {
		c.Write(append(big, '\n'))
		c.(*net.TCPConn).CloseWrite()
	}()
	got, _ := io.ReadAll(c)
	fmt.Println(len(got))
	c.Close()
	ln.Close()
	wg.Wait()
	_, err = net.Dial("tcp", ln.Addr().String())
	fmt.Println(err != nil)
}
