package main

import (
	"fmt"
	"regexp"
)

func main() {
	re := regexp.MustCompile(`(\w+)@(\w+)\.com`)
	s := "contact: bob@example.com, alice@test.com; bad@x.org"
	fmt.Println(re.MatchString(s), re.FindString(s), re.FindStringIndex(s))
	fmt.Println(re.FindStringSubmatch(s))
	fmt.Println(re.FindAllString(s, -1), re.FindAllString(s, 1))
	fmt.Println(re.FindAllStringSubmatch(s, -1))
	fmt.Println(re.FindAllStringIndex(s, -1))
	fmt.Println(re.NumSubexp(), re.String())
	fmt.Println(regexp.MatchString(`^\d{3}-\d{4}$`, "555-1234"))
	fmt.Println(regexp.MatchString(`^\d{3}-\d{4}$`, "5555-1234"))
	_, err := regexp.Compile(`(unclosed`)
	fmt.Println(err)
	_, err = regexp.Compile(`a**`)
	fmt.Println(err)
}
