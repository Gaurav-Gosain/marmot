package main

import (
	"fmt"

	"github.com/iancoleman/strcase"
)

func main() {
	for _, s := range []string{"", "hello", "helloWorld", "HelloWorld", "hello_world", "hello-world", "hello world", "HTTPServer", "getHTTPResponseCode", "user_id_2", "ABC", "a1b2", "__x__y__", "日本語 text", "XMLHttpRequest", "iPhone"} {
		fmt.Printf("%-20q snake=%q kebab=%q camel=%q lcamel=%q scream=%q\n", s, strcase.ToSnake(s), strcase.ToKebab(s), strcase.ToCamel(s), strcase.ToLowerCamel(s), strcase.ToScreamingSnake(s))
	}
	strcase.ConfigureAcronym("API", "api")
	fmt.Println(strcase.ToSnake("myAPIKey"), strcase.ToCamel("my_api_key"), strcase.ToDelimited("hello world", '.'), strcase.ToScreamingDelimited("hello world", '.', "", true))
}
