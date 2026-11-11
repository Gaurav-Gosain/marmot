package main

import "sync"

// Loops that look parallelizable but have side effects or ordering requirements.
var counter int

func next() int { counter++; return counter }

func main() {
	out := make([]int, 10)
	for i := range out {
		out[i] = next() * 10
	}
	println(out[0], out[4], out[9])
	var order []int
	for i := 0; i < 8; i++ {
		order = append(order, i*i)
	}
	println(len(order), order[7])
	m := map[int]int{}
	for i := 0; i < 100; i++ {
		m[i%10] += i
	}
	println(len(m), m[3])
	var wg sync.WaitGroup
	res := make([]int, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				res[i] += j % (i + 1)
			}
		}()
	}
	wg.Wait()
	println(res[0], res[5], res[15])
	var mu sync.Mutex
	total := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			total += i
			mu.Unlock()
		}()
	}
	wg.Wait()
	println(total)
	s := 0
	for i := 0; i < 1000000; i++ {
		s += i & 7
	}
	println(s)
	str := ""
	for i := 0; i < 5; i++ {
		str += string(rune('a' + i))
	}
	println(str)
}
