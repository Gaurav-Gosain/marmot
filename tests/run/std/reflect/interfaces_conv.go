package main

import (
	"fmt"
	"reflect"
)

type Stringer interface{ String() string }
type T struct{}

func (T) String() string { return "T" }

type Num int

func main() {
	st := reflect.TypeOf((*Stringer)(nil)).Elem()
	fmt.Println(reflect.TypeOf(T{}).Implements(st), reflect.TypeOf(1).Implements(st), reflect.TypeOf(&T{}).Implements(st))
	fmt.Println(reflect.TypeOf(Num(1)).ConvertibleTo(reflect.TypeOf(1)), reflect.TypeOf("s").ConvertibleTo(reflect.TypeOf(1)), reflect.TypeOf(1).AssignableTo(reflect.TypeOf(Num(1))))
	v := reflect.ValueOf(Num(7)).Convert(reflect.TypeOf(1.5))
	fmt.Println(v.Float(), v.Type())
	var e any = T{}
	ev := reflect.ValueOf(&e).Elem()
	fmt.Println(ev.Kind(), ev.Elem().Type(), ev.NumMethod())
	fmt.Println(reflect.TypeOf(T{}).NumMethod(), reflect.TypeOf(T{}).Method(0).Name)
	fmt.Println(reflect.TypeOf(1).Comparable(), reflect.TypeOf([]int{}).Comparable())
}
