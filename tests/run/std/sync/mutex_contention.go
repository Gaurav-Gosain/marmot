package main

import "sync"

type SafeMap struct {
	mu sync.Mutex
	m  map[int]int
}

func (s *SafeMap) Inc(k int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k]++
}

func main() {
	s := &SafeMap{m: map[int]int{}}
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				s.Inc(i % 10)
			}
		}()
	}
	wg.Wait()
	t := 0
	for _, v := range s.m {
		t += v
	}
	println(t, s.m[0])
}
