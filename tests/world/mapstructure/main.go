package main

import (
	"fmt"

	"github.com/mitchellh/mapstructure"
)

type Addr struct {
	Street string
	Zip    int `mapstructure:"zip_code"`
}

type Person struct {
	Name   string
	Age    int
	Emails []string
	Addr   Addr
	PAddr  *Addr
	Extra  map[string]any `mapstructure:",remain"`
	Skip   string         `mapstructure:"-"`
	Squash `mapstructure:",squash"`
	Opt    *int `mapstructure:"opt,omitempty"`
	Weak   uint8
}

type Squash struct {
	Embedded string
}

func main() {
	in := map[string]any{
		"name": "Ann", "AGE": 30, "emails": []any{"a@x", "b@y"}, "addr": map[string]any{"street": "Main", "zip_code": "12345"},
		"paddr": map[string]any{"Street": "P"}, "unknown": 1, "other": []int{1}, "embedded": "emb", "weak": 7.0,
	}
	var p Person
	dec, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{Result: &p, WeaklyTypedInput: true, Metadata: &mapstructure.Metadata{}})
	err := dec.Decode(in)
	fmt.Println(err, p.Name, p.Age, p.Emails, p.Addr, *p.PAddr, p.Extra, p.Embedded, p.Weak)
	var strict Person
	err = mapstructure.Decode(map[string]any{"age": "notint", "name": 5, "emails": "x"}, &strict)
	fmt.Println(err)
	var out map[string]any
	fmt.Println(mapstructure.Decode(Person{Name: "Bob", Age: 5, Emails: []string{"q"}, Addr: Addr{"S", 1}}, &out))
	fmt.Println(out["Name"], out["Age"], out["Emails"], out["Addr"])
	var u struct{ A int }
	fmt.Println(mapstructure.Decode(map[string]any{"A": 1, "B": 2}, &u), u)
	d2, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{Result: &u, ErrorUnused: true})
	fmt.Println(d2.Decode(map[string]any{"A": 1, "B": 2}))
	var sl []int
	fmt.Println(mapstructure.WeakDecode([]string{"1", "2", "3"}, &sl), sl)
	var h struct{ D mapstructure.Metadata }
	_ = h
}
