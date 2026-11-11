package main

import (
	"fmt"
	"reflect"
)

type MyInt int
type S struct{ A int }
type I interface{ M() }

func main() {
	vals := []any{1, int8(1), uint(1), 1.5, float32(1), complex(1, 2), "s", true, []int{}, [2]int{}, map[string]int{}, &S{}, S{}, MyInt(1), func() {}, make(chan int), 'x', uintptr(1), struct{}{}, (*int)(nil), [0]int{}}
	for _, v := range vals {
		t := reflect.TypeOf(v)
		fmt.Println(t.String(), t.Kind(), t.Name(), t.Size() > 0 || t.Size() == 0)
	}
	fmt.Println(reflect.TypeOf((*I)(nil)).Elem().Kind(), reflect.TypeOf(S{}).PkgPath(), reflect.TypeOf(MyInt(0)).Name())
	fmt.Println(reflect.TypeOf(nil) == nil)
}
