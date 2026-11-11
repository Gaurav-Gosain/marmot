package main

import (
	"container/list"
	"container/ring"
)

func main() {
	l := list.New()
	e4 := l.PushBack(4)
	e1 := l.PushFront(1)
	l.InsertBefore(3, e4)
	l.InsertAfter(2, e1)
	for e := l.Front(); e != nil; e = e.Next() {
		print(e.Value.(int), " ")
	}
	println(l.Len())
	l.MoveToFront(e4)
	l.Remove(e1)
	for e := l.Back(); e != nil; e = e.Prev() {
		print(e.Value.(int), " ")
	}
	println(l.Len(), l.Front().Value.(int))
	l.Init()
	println(l.Len(), l.Front() == nil)
	r := ring.New(4)
	for i := 0; i < r.Len(); i++ {
		r.Value = i
		r = r.Next()
	}
	sum := 0
	r.Do(func(v any) { sum += v.(int) })
	println(sum, r.Move(2).Value.(int))
}
