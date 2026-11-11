package main

import (
	"fmt"
	"reflect"
)

type P struct {
	Name string
	Age  int
	tags []string
}

func main() {
	p := P{"ann", 30, nil}
	v := reflect.ValueOf(&p).Elem()
	fmt.Println(v.Kind(), v.NumField(), v.Field(0), v.Field(1).Int(), v.Field(0).CanSet(), v.Field(2).CanSet())
	v.Field(0).SetString("bob")
	v.FieldByName("Age").SetInt(41)
	fmt.Println(p)
	fmt.Println(reflect.ValueOf(p).Field(1).CanSet(), reflect.ValueOf(p).FieldByName("Age").Interface())
	x := 5
	xv := reflect.ValueOf(&x).Elem()
	xv.SetInt(9)
	fmt.Println(x, xv.CanAddr(), xv.Addr().Interface() == &x)
	f := 1.5
	reflect.ValueOf(&f).Elem().SetFloat(2.5)
	b := false
	reflect.ValueOf(&b).Elem().SetBool(true)
	s := "a"
	reflect.ValueOf(&s).Elem().Set(reflect.ValueOf("zz"))
	fmt.Println(f, b, s)
	fmt.Println(reflect.ValueOf(nil).IsValid(), reflect.Zero(reflect.TypeOf(0)).Int(), reflect.ValueOf((*int)(nil)).IsNil())
}
