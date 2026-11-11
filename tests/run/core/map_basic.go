package main

func main() {
	m := map[string]int{"a": 1, "b": 2}
	println(len(m), m["a"], m["b"], m["zz"])
	m["c"] = 3
	m["a"] = 10
	println(len(m), m["a"])
	v, ok := m["c"]
	println(v, ok)
	v, ok = m["nope"]
	println(v, ok)
	delete(m, "a")
	delete(m, "absent")
	println(len(m))
	_, ok = m["a"]
	println(ok)
	m["x"]++
	m["x"] += 5
	println(m["x"])
	var nm map[string]int
	println(len(nm), nm["q"], nm == nil)
	e := map[int]bool{}
	println(len(e), e == nil)
	mk := make(map[int]string, 100)
	println(len(mk))
}
