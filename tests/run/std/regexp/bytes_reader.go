package main

import (
	"fmt"
	"regexp"
	"strings"
)

func main() {
	re := regexp.MustCompile(`b+`)
	fmt.Println(re.Match([]byte("abbbc")), string(re.Find([]byte("abbbc"))), re.FindIndex([]byte("abbbc")))
	fmt.Println(re.MatchReader(strings.NewReader("xxbxx")), re.FindReaderIndex(strings.NewReader("xxbbx")))
	fmt.Printf("%q\n", re.FindAll([]byte("abbcbbb"), -1))
	re2 := regexp.MustCompile(`(?i)\bgo(lang)?\b`)
	fmt.Println(re2.FindAllString("Go golang GOLANG gopher", -1))
	lit, complete := regexp.MustCompile(`abc+`).LiteralPrefix()
	fmt.Println(lit, complete)
	re3 := regexp.MustCompile(`a`)
	re3.Longest()
	fmt.Println(re3.MatchString("a"))
}
