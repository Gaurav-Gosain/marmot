package main

import (
	"fmt"
	"os"
	"strings"
	"text/template"
)

type T struct{ N int }

func (T) Fail() (string, error) { return "", fmt.Errorf("method failed") }

func main() {
	_, err := template.New("x").Parse("{{.Foo")
	fmt.Println(err != nil)
	_, err = template.New("x").Parse("{{nofunc 1}}")
	fmt.Println(err)
	t := template.Must(template.New("e").Parse("{{.Missing}}"))
	err = t.Execute(os.Stdout, T{1})
	fmt.Println()
	fmt.Println(err != nil)
	t2 := template.Must(template.New("e2").Parse("{{.Fail}}"))
	err = t2.Execute(os.Stdout, T{})
	fmt.Println()
	fmt.Println(err != nil, strings.Contains(err.Error(), "method failed"))
	t3 := template.Must(template.New("e3").Option("missingkey=error").Parse("{{.k}}"))
	err = t3.Execute(os.Stdout, map[string]int{})
	fmt.Println(err != nil)
	t4 := template.Must(template.New("e4").Parse("{{.k}}\n"))
	t4.Execute(os.Stdout, map[string]int{})
}
