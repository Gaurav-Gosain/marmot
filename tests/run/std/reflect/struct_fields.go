package main

import (
	"fmt"
	"reflect"
)

type Inner struct{ Z int }

type T struct {
	A int    `json:"a" db:"col_a"`
	B string `json:"b,omitempty"`
	c bool
	Inner
	P *int
	L []string
}

func main() {
	t := reflect.TypeOf(T{})
	fmt.Println(t.NumField(), t.Kind())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fmt.Println(i, f.Name, f.Type, f.Tag.Get("json"), f.Tag.Get("db"), f.IsExported(), f.Anonymous, f.Offset > 0 || i == 0)
	}
	f, ok := t.FieldByName("Z")
	fmt.Println(f.Name, ok, f.Index)
	_, ok = t.FieldByName("nope")
	fmt.Println(ok)
	tag, ok := t.Field(0).Tag.Lookup("db")
	fmt.Println(tag, ok)
}
