package main

import (
	"fmt"
	"reflect"
	"sort"
)

func main() {
	s := []int{10, 20, 30}
	v := reflect.ValueOf(s)
	fmt.Println(v.Len(), v.Cap(), v.Index(1).Int(), v.Slice(1, 3).Len())
	v.Index(0).SetInt(99)
	fmt.Println(s)
	v2 := reflect.Append(v, reflect.ValueOf(40))
	fmt.Println(v2.Len(), v2.Interface())
	arr := [3]string{"a", "b", "c"}
	av := reflect.ValueOf(&arr).Elem()
	av.Index(2).SetString("z")
	fmt.Println(arr, av.Len(), av.Type())
	m := map[string]int{"x": 1, "y": 2}
	mv := reflect.ValueOf(m)
	keys := mv.MapKeys()
	var ks []string
	for _, k := range keys {
		ks = append(ks, k.String())
	}
	sort.Strings(ks)
	fmt.Println(ks, mv.MapIndex(reflect.ValueOf("y")), mv.Len())
	mv.SetMapIndex(reflect.ValueOf("z"), reflect.ValueOf(3))
	mv.SetMapIndex(reflect.ValueOf("x"), reflect.Value{})
	fmt.Println(m)
	it := mv.MapRange()
	n := 0
	for it.Next() {
		n += int(it.Value().Int())
	}
	fmt.Println(n)
}
