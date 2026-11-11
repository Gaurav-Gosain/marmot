package main

import "sync"

func main() {
	created := 0
	p := sync.Pool{New: func() any { created++; return make([]byte, 0, 16) }}
	b := p.Get().([]byte)
	b = append(b, "hi"...)
	println(len(b), cap(b), created)
	p.Put(b[:0])
	c := p.Get().([]byte)
	println(len(c), created <= 2)
	var empty sync.Pool
	println(empty.Get() == nil)
	empty.Put(5)
	v := empty.Get()
	println(v == nil || v.(int) == 5)
}
