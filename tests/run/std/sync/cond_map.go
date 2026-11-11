package main

import "sync"

func main() {
	var m sync.Map
	m.Store("a", 1)
	m.Store("b", 2)
	v, ok := m.Load("a")
	println(v.(int), ok)
	_, ok = m.Load("z")
	println(ok)
	act, loaded := m.LoadOrStore("a", 9)
	println(act.(int), loaded)
	m.Delete("a")
	n := 0
	m.Range(func(k, v any) bool { n++; return true })
	println(n)
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	ready := false
	done := make(chan bool)
	go func() {
		mu.Lock()
		for !ready {
			cond.Wait()
		}
		mu.Unlock()
		done <- true
	}()
	mu.Lock()
	ready = true
	cond.Broadcast()
	mu.Unlock()
	<-done
	println("cond ok")
}
