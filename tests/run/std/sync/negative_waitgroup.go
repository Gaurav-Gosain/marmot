package main

import "sync"

func main() {
	defer func() { println(recover().(string)) }()
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Done()
	wg.Done()
}
