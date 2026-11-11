package main

type Rec struct {
	id   int
	name string
	tags []string
}

func main() {
	m := map[int]*Rec{}
	for i := 0; i < 20000; i++ {
		m[i] = &Rec{i, "name", []string{"a", "b"}}
	}
	for i := 0; i < 20000; i += 2 {
		delete(m, i)
	}
	for i := 0; i < 50; i++ {
		junk := make([]*Rec, 1000)
		for j := range junk {
			junk[j] = &Rec{id: j}
		}
	}
	s := 0
	for k, v := range m {
		if v.id != k || len(v.tags) != 2 || v.name != "name" {
			println("corrupt", k)
			return
		}
		s += v.id
	}
	println(len(m), s)
}
