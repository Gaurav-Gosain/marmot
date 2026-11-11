package main

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

type Item struct {
	Name  string   `yaml:"name"`
	Qty   int      `yaml:"qty"`
	Tags  []string `yaml:"tags,omitempty"`
	Price float64  `yaml:"price"`
}

type Order struct {
	ID    int               `yaml:"id"`
	Items []Item            `yaml:"items"`
	Meta  map[string]string `yaml:"meta"`
	Note  *string           `yaml:"note"`
}

const doc = `
id: 42
items:
  - name: apple
    qty: 3
    tags: [fruit, red]
    price: 0.5
  - {name: "kiwi", qty: 12, price: 1.25}
meta:
  source: web
  "quoted key": 'single'
note: |
  multi
  line
anchors:
  base: &b {x: 1}
  copy: *b
`

func main() {
	var o Order
	err := yaml.Unmarshal([]byte(doc), &o)
	fmt.Println(err, o.ID, len(o.Items), o.Items[0], o.Items[1], *o.Note)
	var keys []string
	for k := range o.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println(keys)
	out, err := yaml.Marshal(o)
	fmt.Println(err)
	fmt.Print(string(out))
	var generic map[string]any
	fmt.Println(yaml.Unmarshal([]byte(doc), &generic), generic["anchors"])
	var node yaml.Node
	fmt.Println(yaml.Unmarshal([]byte("a: [1, 2]\nb: x"), &node), node.Kind, len(node.Content[0].Content))
	for _, bad := range []string{"a: [1, 2", "a: b: c", "\tx: 1", "- a\nb: 1", "a: 1\na: 2"} {
		var v any
		fmt.Printf("%q -> %v\n", bad, yaml.Unmarshal([]byte(bad), &v))
	}
	var types any
	yaml.Unmarshal([]byte("[1, 1.5, true, null, 'str', 0x1f, 2001-12-14, ~]"), &types)
	for _, e := range types.([]any) {
		fmt.Printf("%T %v\n", e, e)
	}
}
