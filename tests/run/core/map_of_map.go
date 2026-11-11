package main

func main() {
	m := map[string]map[string]int{}
	if m["a"] == nil {
		m["a"] = map[string]int{}
	}
	m["a"]["x"] = 1
	m["a"]["y"]++
	println(len(m), len(m["a"]), m["a"]["x"], m["a"]["y"])
	println(m["b"]["z"], len(m["b"]))
	ms := map[int][]string{}
	ms[1] = append(ms[1], "a")
	ms[1] = append(ms[1], "b")
	println(len(ms[1]), len(ms[2]))
	type pt struct{ x int }
	mp := map[string]*pt{"p": {1}}
	mp["p"].x++
	println(mp["p"].x)
	ma := map[string][2]int{"a": {1, 2}}
	v := ma["a"]
	v[0] = 100
	println(ma["a"][0], v[0])
}
