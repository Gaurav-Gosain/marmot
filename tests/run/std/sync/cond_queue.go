package main

import "sync"

type Queue struct {
	mu    sync.Mutex
	cond  *sync.Cond
	items []int
}

func NewQueue() *Queue {
	q := &Queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *Queue) Put(v int) {
	q.mu.Lock()
	q.items = append(q.items, v)
	q.mu.Unlock()
	q.cond.Signal()
}

func (q *Queue) Get() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 {
		q.cond.Wait()
	}
	v := q.items[0]
	q.items = q.items[1:]
	return v
}

func main() {
	q := NewQueue()
	done := make(chan int)
	go func() {
		s := 0
		for i := 0; i < 100; i++ {
			s += q.Get()
		}
		done <- s
	}()
	for i := 1; i <= 100; i++ {
		q.Put(i)
	}
	println(<-done)
}
