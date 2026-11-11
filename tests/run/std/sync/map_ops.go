package main

import "sync"

func main() {
	var m sync.Map
	for i := 0; i < 10; i++ {
		m.Store(i, i*i)
	}
	v, ok := m.Load(3)
	println(v.(int), ok)
	_, ok = m.Load(99)
	println(ok)
	act, loaded := m.LoadOrStore(3, 0)
	println(act.(int), loaded)
	act, loaded = m.LoadOrStore(50, 7)
	println(act.(int), loaded)
	v, loaded = m.LoadAndDelete(4)
	println(v.(int), loaded)
	prev, loaded := m.Swap(5, 1000)
	println(prev.(int), loaded)
	println(m.CompareAndSwap(5, 1000, 1), m.CompareAndSwap(5, 1000, 2), m.CompareAndDelete(5, 1))
	n, sum := 0, 0
	m.Range(func(k, v any) bool { n++; sum += v.(int); return true })
	println(n, sum)
	m.Clear()
	n = 0
	m.Range(func(k, v any) bool { n++; return true })
	println(n)
}
