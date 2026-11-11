package main

import (
	"fmt"
	"regexp"
)

func main() {
	re := regexp.MustCompile(`,\s*`)
	fmt.Printf("%q\n", re.Split("a, b,c,   d", -1))
	fmt.Printf("%q\n", re.Split("a, b,c,   d", 2))
	fmt.Printf("%q\n", regexp.MustCompile(`x*`).Split("axbxc", -1))
	fmt.Printf("%q\n", re.Split("", -1))
	named := regexp.MustCompile(`(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})`)
	m := named.FindStringSubmatch("date: 2024-03-15")
	for i, n := range named.SubexpNames() {
		fmt.Printf("%d %q %q\n", i, n, m[i])
	}
	fmt.Println(named.SubexpIndex("month"), named.SubexpIndex("none"))
	fmt.Println(regexp.QuoteMeta("a.b*c[d]"))
}
