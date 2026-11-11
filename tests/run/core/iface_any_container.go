package main

func main() {
	m := map[interface{}]string{}
	m[1] = "int"
	m["a"] = "str"
	m[1.5] = "float"
	m[[2]int{1, 2}] = "arr"
	m[true] = "bool"
	m[struct{ x int }{1}] = "struct"
	println(len(m))
	println(m[1], m["a"], m[1.5], m[[2]int{1, 2}], m[true], m[struct{ x int }{1}])
	println(m[int8(1)] == "", m[2] == "")
	var nilk interface{}
	m[nilk] = "nil"
	println(m[nil])
	delete(m, 1)
	println(len(m))
}
