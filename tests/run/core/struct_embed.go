package main

type Base struct {
	ID   int
	Name string
}

func (b Base) Describe() string  { return b.Name }
func (b *Base) SetName(n string) { b.Name = n }

type Mid struct {
	Base
	Level int
}

type Top struct {
	*Mid
	Extra string
}

func main() {
	t := Top{&Mid{Base{1, "base"}, 2}, "x"}
	println(t.ID, t.Name, t.Level, t.Extra)
	t.SetName("renamed")
	println(t.Describe(), t.Base.Name, t.Mid.Base.Name)
	t.ID = 9
	println(t.Mid.ID)
	m := Mid{Base{5, "five"}, 0}
	m.SetName("FIVE")
	println(m.Describe())
	f := m.Describe
	m.SetName("changed")
	println(f(), m.Describe())
}
