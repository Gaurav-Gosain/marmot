package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	srv, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer srv.Close()
	go func() {
		buf := make([]byte, 64)
		for {
			n, addr, err := srv.ReadFrom(buf)
			if err != nil {
				return
			}
			srv.WriteTo(append([]byte("re:"), buf[:n]...), addr)
		}
	}()
	c, err := net.Dial("udp", srv.LocalAddr().String())
	if err != nil {
		fmt.Println(err)
		return
	}
	defer c.Close()
	for _, m := range []string{"ping", "", "a longer datagram with spaces"} {
		c.Write([]byte(m))
		buf := make([]byte, 64)
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := c.Read(buf)
		fmt.Printf("%q %v\n", buf[:n], err)
	}
	uc := c.(*net.UDPConn)
	fmt.Println(uc.RemoteAddr().Network(), uc.LocalAddr().(*net.UDPAddr).IP)
	a, err := net.ResolveUDPAddr("udp", "127.0.0.1:9999")
	fmt.Println(a, err)
	t, err := net.ResolveTCPAddr("tcp", "[::1]:80")
	fmt.Println(t, err, t.IP.IsLoopback())
	_, err = net.ResolveTCPAddr("tcp", "nonsense")
	fmt.Println(err != nil)
}
