package main

type T struct{}
type I interface{ M() }

func try(f func()) {
	defer func() {
		println(recover().(error).Error())
	}()
	f()
}

func main() {
	var e interface{} = 1
	try(func() { _ = e.(string) })
	try(func() { _ = e.(I) })
	var n interface{}
	try(func() { _ = n.(int) })
	try(func() { _ = n.(I) })
	var s interface{} = T{}
	try(func() { _ = s.(*T) })
	try(func() { _ = s.(I) })
}
