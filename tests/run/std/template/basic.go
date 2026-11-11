package main

import (
	"os"
	"text/template"
)

type Item struct {
	Name  string
	Price float64
	Qty   int
}

func (i Item) Total() float64 { return i.Price * float64(i.Qty) }

func main() {
	t := template.Must(template.New("t").Parse(`Hello, {{.Name}}!
{{range $i, $it := .Items}}{{$i}}: {{$it.Name}} x{{$it.Qty}} = {{printf "%.2f" $it.Total}}
{{else}}no items
{{end}}{{if .VIP}}VIP{{else}}regular{{end}} {{len .Items}} items
`))
	err := t.Execute(os.Stdout, map[string]any{
		"Name":  "World",
		"VIP":   true,
		"Items": []Item{{"apple", 0.5, 4}, {"pear", 1.25, 2}},
	})
	println(err == nil)
	t.Execute(os.Stdout, map[string]any{"Name": "Empty", "VIP": false, "Items": []Item{}})
}
