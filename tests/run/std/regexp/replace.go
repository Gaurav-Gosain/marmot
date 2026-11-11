package main

import (
	"fmt"
	"regexp"
	"strings"
)

func main() {
	re := regexp.MustCompile(`(\d+)-(\d+)`)
	s := "10-20 and 30-40"
	fmt.Println(re.ReplaceAllString(s, "$2-$1"))
	fmt.Println(re.ReplaceAllString(s, "${1}x$2"))
	fmt.Println(re.ReplaceAllString(s, "$1x"))
	fmt.Println(re.ReplaceAllLiteralString(s, "$1"))
	fmt.Println(re.ReplaceAllStringFunc(s, func(m string) string { return "<" + strings.ToUpper(m) + ">" }))
	fmt.Println(regexp.MustCompile(`a*`).ReplaceAllString("baaac", "X"))
	fmt.Println(regexp.MustCompile(`\s+`).ReplaceAllString("a  b \t\n c", " "))
	fmt.Println(string(re.ReplaceAll([]byte(s), []byte("[$0]"))))
	fmt.Println(re.ExpandString(nil, "$1+$2", s, re.FindStringSubmatchIndex(s)), string(re.ExpandString(nil, "$1+$2", s, re.FindStringSubmatchIndex(s))))
}
