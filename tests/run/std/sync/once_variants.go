package main

import "sync"

var cfg = sync.OnceValue(func() int { println("init cfg"); return 42 })
var pair = sync.OnceValues(func() (string, error) { println("init pair"); return "p", nil })

func main() {
	var o sync.Once
	var wg sync.WaitGroup
	n := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.Do(func() { n++ })
		}()
	}
	wg.Wait()
	println(n)
	println(cfg(), cfg())
	a, err := pair()
	b, _ := pair()
	println(a, b, err == nil)
	f := sync.OnceFunc(func() { println("once func") })
	f()
	f()
	defer func() { println(recover().(string)) }()
	var o2 sync.Once
	o2.Do(func() { panic("in once") })
}
