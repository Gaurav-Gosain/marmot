package main

import (
	"fmt"

	"github.com/tidwall/gjson"
)

const doc = `{
  "name": {"first": "Tom", "last": "Anderson"},
  "age": 37,
  "children": ["Sara", "Alex", "Jack"],
  "fav.movie": "Deer Hunter",
  "friends": [
    {"first": "Dale", "last": "Murphy", "age": 44, "nets": ["ig", "fb", "tw"]},
    {"first": "Roger", "last": "Craig", "age": 68, "nets": ["fb", "tw"]},
    {"first": "Jane", "last": "Murphy", "age": 47, "nets": ["ig", "tw"]}
  ],
  "n": 1.5e3, "t": true, "z": null, "esc": "a\"b\\cé"
}`

func main() {
	for _, p := range []string{"name.last", "age", "children", "children.#", "children.1", "friends.#.first", "friends.#(last==\"Murphy\").first", "friends.#(last==\"Murphy\")#.first",
		"friends.#(age>45)#.last", "friends.0.nets.#", `fav\.movie`, "n", "t", "z", "esc", "missing", "name.@reverse", "children.@reverse", "friends.#.nets|@flatten", "@this.age", "friends.#(nets.#(==\"ig\"))#.first", "age|@pretty:{\"sortKeys\":true}"} {
		r := gjson.Get(doc, p)
		fmt.Printf("%-40s %-8v exists=%v %q\n", p, r.Type, r.Exists(), r.String())
	}
	r := gjson.Get(doc, "friends")
	n := 0
	r.ForEach(func(k, v gjson.Result) bool {
		n += int(v.Get("age").Int())
		return true
	})
	fmt.Println(n, r.IsArray(), len(r.Array()), gjson.Get(doc, "name").Map()["first"].String())
	fmt.Println(gjson.Valid(doc), gjson.Valid(`{"a":}`), gjson.Parse(`[1,2,{"a":3}]`).Get("2.a"))
	res := gjson.GetMany(doc, "age", "name.first", "t")
	fmt.Println(res[0].Int(), res[1].Str, res[2].Bool(), gjson.Get(doc, "n").Float(), gjson.Get(doc, "age").Uint())
	gjson.ForEachLine("{\"a\":1}\n{\"a\":2}\n", func(l gjson.Result) bool { fmt.Print(l.Get("a"), " "); return true })
	fmt.Println()
	m, ok := gjson.Parse(doc).Value().(map[string]any)
	fmt.Println(ok, len(m))
	big := "["
	for i := 0; i < 2000; i++ {
		if i > 0 {
			big += ","
		}
		big += fmt.Sprintf(`{"i":%d,"s":"v%d"}`, i, i)
	}
	big += "]"
	fmt.Println(gjson.Get(big, "#"), gjson.Get(big, "1999.s"), gjson.Get(big, `#(i==1234).s`), gjson.Get(big, "#(i>1990)#.i"))
}
