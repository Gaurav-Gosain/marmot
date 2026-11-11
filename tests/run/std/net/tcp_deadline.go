package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	hold := make(chan struct{})
	go func() {
		c, _ := ln.Accept()
		<-hold
		c.Close()
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		fmt.Println(err)
		return
	}
	c.SetReadDeadline(time.Now().Add(40 * time.Millisecond))
	start := time.Now()
	_, err = c.Read(make([]byte, 1))
	var ne net.Error
	fmt.Println(errors.As(err, &ne) && ne.Timeout(), errors.Is(err, os.ErrDeadlineExceeded), time.Since(start) >= 35*time.Millisecond)
	c.SetReadDeadline(time.Time{})
	close(hold)
	n, err := c.Read(make([]byte, 1))
	fmt.Println(n, err)
	c.SetWriteDeadline(time.Now().Add(-time.Second))
	_, err = c.Write([]byte("x"))
	fmt.Println(errors.Is(err, os.ErrDeadlineExceeded))
	c.Close()
	_, err = c.Write([]byte("x"))
	fmt.Println(errors.Is(err, net.ErrClosed))
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	ln2.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Millisecond))
	_, err = ln2.Accept()
	fmt.Println(errors.Is(err, os.ErrDeadlineExceeded))
	ln2.Close()
	_, err = ln2.Accept()
	fmt.Println(errors.Is(err, net.ErrClosed))
	d := net.Dialer{Timeout: 50 * time.Millisecond}
	_, err = d.Dial("tcp", "127.0.0.1:1")
	fmt.Println(err != nil)
}
