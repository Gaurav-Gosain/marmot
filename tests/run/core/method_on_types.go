package main

type MyInt int

func (m MyInt) Double() MyInt { return m * 2 }
func (m *MyInt) Inc()         { *m++ }

type Str string

func (s Str) Len() int { return len(s) }

type IntSlice []int

func (s IntSlice) Sum() int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}

func (s *IntSlice) Push(v int) { *s = append(*s, v) }

type FuncT func(int) int

func (f FuncT) Apply(x int) int { return f(x) }

type MapT map[string]int

func (m MapT) Add(k string) { m[k]++ }

func main() {
	m := MyInt(4)
	m.Inc()
	println(m.Double(), m)
	println(Str("hello").Len())
	var s IntSlice
	s.Push(1)
	s.Push(2)
	s.Push(3)
	println(s.Sum(), len(s))
	f := FuncT(func(x int) int { return x + 1 })
	println(f.Apply(1))
	mt := MapT{}
	mt.Add("a")
	mt.Add("a")
	println(mt["a"])
}
