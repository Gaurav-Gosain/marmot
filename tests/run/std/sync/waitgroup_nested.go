package main

import "sync"

func main() {
	var outer sync.WaitGroup
	results := make([]int, 5)
	for i := range results {
		outer.Add(1)
		go func() {
			defer outer.Done()
			var inner sync.WaitGroup
			for j := 0; j < 10; j++ {
				inner.Add(1)
				go func() {
					defer inner.Done()
					results[i] += j
				}()
				inner.Wait()
			}
		}()
	}
	outer.Wait()
	println(results[0], results[4])
	var wg sync.WaitGroup
	wg.Wait()
	wg.Go(func() { println("wg.Go") })
	wg.Wait()
}
