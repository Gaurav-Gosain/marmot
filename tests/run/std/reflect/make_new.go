package main

import (
	"fmt"
	"reflect"
)

type P struct{ X, Y int }

func main() {
	nv := reflect.New(reflect.TypeOf(P{}))
	nv.Elem().Field(0).SetInt(3)
	nv.Elem().Field(1).SetInt(4)
	fmt.Println(nv.Elem().Interface(), nv.Type())
	sl := reflect.MakeSlice(reflect.TypeOf([]string{}), 2, 5)
	sl.Index(0).SetString("a")
	fmt.Println(sl.Len(), sl.Cap(), sl.Interface())
	mp := reflect.MakeMap(reflect.TypeOf(map[int]string{}))
	mp.SetMapIndex(reflect.ValueOf(1), reflect.ValueOf("one"))
	fmt.Println(mp.Interface(), mp.Len())
	ch := reflect.MakeChan(reflect.TypeOf(make(chan int)), 2)
	ch.Send(reflect.ValueOf(5))
	r, ok := ch.Recv()
	fmt.Println(r.Int(), ok, ch.Len())
	fmt.Println(reflect.TypeOf(reflect.Zero(reflect.TypeOf(P{})).Interface()), reflect.Indirect(reflect.ValueOf(&P{1, 2})).Field(1))
	fmt.Println(reflect.PtrTo(reflect.TypeOf(0)), reflect.TypeFor[[]int]())
}
