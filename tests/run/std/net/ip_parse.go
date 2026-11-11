package main

import (
	"fmt"
	"net"
	"net/netip"
)

func main() {
	for _, s := range []string{"127.0.0.1", "::1", "192.168.1.1", "10.0.0.256", "2001:db8::1", "::ffff:1.2.3.4", "", "1.2.3", "fe80::1%eth0"} {
		ip := net.ParseIP(s)
		fmt.Printf("%-16q %v loop=%v priv=%v v4=%v\n", s, ip, ip.IsLoopback(), ip.IsPrivate(), ip.To4() != nil)
	}
	_, n, err := net.ParseCIDR("192.168.4.7/22")
	fmt.Println(n, err, n.Contains(net.ParseIP("192.168.7.255")), n.Contains(net.ParseIP("192.168.8.0")), n.Mask, n.IP)
	ones, bits := n.Mask.Size()
	fmt.Println(ones, bits, net.CIDRMask(20, 32), net.IPv4(1, 2, 3, 4).Equal(net.ParseIP("1.2.3.4")))
	_, err = fmt.Println(net.ParseMAC("00:1a:2b:3c:4d:5e"))
	_, err = net.ParseMAC("zz")
	fmt.Println(err != nil)
	h, p, err := net.SplitHostPort("[::1]:8080")
	fmt.Println(h, p, err, net.JoinHostPort("::1", "80"), net.JoinHostPort("a.b", "1"))
	_, _, err = net.SplitHostPort("noport")
	fmt.Println(err)
	a, _ := netip.ParseAddr("2001:db8::1")
	pf := netip.MustParsePrefix("10.1.0.0/16")
	fmt.Println(a, a.Is6(), pf.Contains(netip.MustParseAddr("10.1.255.1")), pf.Masked(), pf.Bits(), a.Next(), netip.MustParseAddrPort("1.2.3.4:80").Port())
}
