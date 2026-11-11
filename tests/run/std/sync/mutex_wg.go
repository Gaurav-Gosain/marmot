package main

import "sync"

func main() {
	var mu sync.Mutex
	var wg sync.WaitGroup
	counter := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	println(counter)
	var rw sync.RWMutex
	rw.RLock()
	rw.RLock()
	rw.RUnlock()
	rw.RUnlock()
	rw.Lock()
	rw.Unlock()
	println(mu.TryLock(), mu.TryLock())
}
