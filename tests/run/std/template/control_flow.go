package main

import (
	"os"
	"text/template"
)

func main() {
	t := template.Must(template.New("c").Parse(`{{range .}}{{if eq . 3}}{{continue}}{{end}}{{if gt . 5}}{{break}}{{end}}{{.}} {{end}}
{{range $k, $v := .M}}{{$k}}={{$v}};{{end}}
{{$x := 1}}{{if true}}{{$x = 2}}{{end}}{{$x}}
{{range $i := 3}}{{$i}}{{end}}
`))
	data := map[string]any{}
	_ = data
	err := t.Execute(os.Stdout, []int{1, 2, 3, 4, 6, 7})
	println(err != nil)
	t2 := template.Must(template.New("d").Parse(`{{range $k, $v := .}}{{$k}}={{$v}};{{end}}{{"\n"}}`))
	t2.Execute(os.Stdout, map[string]int{"z": 1, "a": 2, "m": 3})
}
