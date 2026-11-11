package main

import (
	"os"
	"text/template"
)

func main() {
	root := template.Must(template.New("root").Parse(`{{define "item"}}<{{.}}>{{end}}{{define "list"}}{{range .}}{{template "item" .}}{{end}}{{end}}start {{template "list" .}} end
`))
	root.Execute(os.Stdout, []string{"a", "b", "c"})
	root.ExecuteTemplate(os.Stdout, "item", 42)
	os.Stdout.WriteString("\n")
	println(root.Lookup("item") != nil, root.Lookup("zzz") == nil, len(root.Templates()))
	t2 := template.Must(template.New("w").Parse("{{- \"a\" -}}   {{ \"b\" }}  {{- \"c\" }}\n"))
	t2.Execute(os.Stdout, nil)
	t3 := template.Must(template.New("d").Delims("<<", ">>").Parse("<<.>> {{.}}\n"))
	t3.Execute(os.Stdout, 5)
}
