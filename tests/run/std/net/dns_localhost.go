package main

import (
	"context"
	"fmt"
	"net"
	"time"
)

func main() {
	r := &net.Resolver{PreferGo: true}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := r.LookupHost(ctx, "localhost")
	fmt.Println(len(addrs) > 0, err)
	for _, a := range addrs {
		ip := net.ParseIP(a)
		fmt.Println(ip != nil && ip.IsLoopback())
	}
	ips, err := r.LookupIPAddr(ctx, "127.0.0.1")
	fmt.Println(ips, err)
	port, err := r.LookupPort(ctx, "tcp", "http")
	fmt.Println(port, err)
	port, err = net.LookupPort("tcp", "8080")
	fmt.Println(port, err)
	_, err = r.LookupHost(ctx, "definitely-not-a-real-host.invalid")
	fmt.Println(err != nil)
}
