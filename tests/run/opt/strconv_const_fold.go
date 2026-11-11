package main

import (
	"fmt"
	"strconv"
	"strings"
)

func main() {
	n, err := strconv.Atoi("12345")
	fmt.Println(n, err)
	_, err = strconv.Atoi("12a")
	fmt.Println(err)
	_, err = strconv.Atoi("99999999999999999999")
	fmt.Println(err)
	f, err := strconv.ParseFloat("1e400", 64)
	fmt.Println(f, err)
	b, err := strconv.ParseBool("maybe")
	fmt.Println(b, err)
	fmt.Println(strconv.Itoa(-0), strconv.Quote("a\tb\x00é"), strconv.FormatInt(-255, 16), strconv.FormatFloat(1.0/3, 'g', 5, 64))
	fmt.Println(strings.Repeat("ab", 3), strings.ToUpper("héllo"), strings.Contains("abc", ""), strings.Index("chicken", "ken"), strings.Split("a,b,,c", ","), strings.Fields(" a  b "), strings.TrimSpace("  x  "), strings.Replace("aaa", "a", "b", 2), strings.Title("go lang"))
	fmt.Println(strings.EqualFold("Go", "GO"), strings.HasPrefix("golang", "go"), strings.Count("cheese", "e"), strings.LastIndex("go gopher", "go"), strings.Join([]string{"x", "y"}, "-"))
	u, err := strconv.ParseUint("-1", 10, 64)
	fmt.Println(u, err)
	i8, err := strconv.ParseInt("200", 10, 8)
	fmt.Println(i8, err)
	for _, s := range []string{"0x1f", "0b101", "1_000", "-0", "+7", " 1", ""} {
		v, err := strconv.ParseInt(s, 0, 64)
		fmt.Println(s, v, err)
	}
}
