package main

import (
	"fmt"
	"reflect"
	"strings"
)

type Leaf struct {
	N int
	S string
}
type Tree struct {
	Name     string
	Leaves   []Leaf
	Children map[string]*Tree
	Ptr      *Leaf
	Any      any
}

func walk(v reflect.Value, depth int, out *[]string) {
	ind := strings.Repeat(" ", depth)
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			*out = append(*out, ind+v.Type().Field(i).Name+":")
			walk(v.Field(i), depth+1, out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i), depth+1, out)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			*out = append(*out, ind+"key "+k.String())
			walk(v.MapIndex(k), depth+1, out)
		}
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			*out = append(*out, ind+"nil")
		} else {
			walk(v.Elem(), depth, out)
		}
	default:
		*out = append(*out, ind+fmt.Sprint(v.Interface()))
	}
}

func main() {
	t := Tree{"root", []Leaf{{1, "a"}, {2, "b"}}, map[string]*Tree{"c": {Name: "child"}}, &Leaf{9, "p"}, 42}
	var out []string
	walk(reflect.ValueOf(t), 0, &out)
	fmt.Println(strings.Join(out, "\n"))
}
