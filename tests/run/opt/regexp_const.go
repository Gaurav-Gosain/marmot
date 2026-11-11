package main

import (
	"fmt"
	"regexp"
)

var emailRe = regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$`)
var wordRe = regexp.MustCompile(`\b\w+\b`)
var dateRe = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)
var alt = regexp.MustCompile(`cat|category|dog`)
var lazy = regexp.MustCompile(`a.*?b`)
var greedy = regexp.MustCompile(`a.*b`)
var uni = regexp.MustCompile(`(?i)straße|\p{Greek}+`)

func main() {
	for _, s := range []string{"bob@example.com", "bad@", "A@b.co", "x@y.z", "", "a.b+c@d-e.org"} {
		fmt.Println(s, emailRe.MatchString(s))
	}
	fmt.Println(wordRe.FindAllString("hello, wörld! it's 42", -1))
	fmt.Println(dateRe.FindStringSubmatch("on 2024-03-15 and 2025-01-02"), dateRe.ReplaceAllString("2024-03-15", "$3/$2/$1"))
	fmt.Println(alt.FindAllString("category cat dog", -1))
	fmt.Println(lazy.FindString("aXbXb"), greedy.FindString("aXbXb"))
	fmt.Println(uni.FindAllString("STRASSE Straße αβγ abc", -1))
	fmt.Println(dateRe.FindAllStringSubmatchIndex("2024-03-15 2025-01-02", -1))
	re := regexp.MustCompile(`(a+)(b*)`)
	fmt.Printf("%q %q\n", re.FindStringSubmatch("aaabb"), re.Split("xaabyaz", -1))
}
