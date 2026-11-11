package main

import (
	"os"
	"strings"
	"text/template"
)

func main() {
	funcs := template.FuncMap{"upper": strings.ToUpper, "add": func(a, b int) int { return a + b }, "join": strings.Join}
	t := template.Must(template.New("p").Funcs(funcs).Parse(
		`{{.S | upper}} {{add 1 2}} {{.S | printf "%q"}} {{join .L ","}} {{index .L 1}} {{index .M "k"}}
{{with .Missing}}has{{else}}none{{end}} {{and .A .B}} {{or .A .B}} {{not .A}} {{eq .N 5}} {{lt .N 10}} {{ne .S "x"}}
{{- /* comment */ -}}
{{define "sub"}}[{{.}}]{{end}} {{template "sub" .S}} {{html "<b>"}} {{len .S}} {{slice .S 1 3}}
`))
	err := t.Execute(os.Stdout, map[string]any{"S": "hello", "L": []string{"a", "b"}, "M": map[string]int{"k": 7}, "A": true, "B": false, "N": 5})
	println(err == nil)
}
