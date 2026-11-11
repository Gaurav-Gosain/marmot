package main

import "sync"

func main() {
	var rw sync.RWMutex
	data := 0
	var wg sync.WaitGroup
	reads := make([]int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rw.RLock()
				_ = data
				reads[i]++
				rw.RUnlock()
			}
		}()
	}
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rw.Lock()
				data++
				rw.Unlock()
			}
		}()
	}
	wg.Wait()
	println(data, reads[0], reads[9])
	println(rw.TryLock(), rw.TryRLock())
	rw.Unlock()
	println(rw.TryRLock(), rw.TryLock())
}
