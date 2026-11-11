package main

import "runtime"

func main() {
	stop := false
	cnt := 0
	done := make(chan bool)
	go func() {
		for !stop {
			runtime.Gosched()
			cnt++
		}
		done <- true
	}()
	for i := 0; i < 100; i++ {
		runtime.Gosched()
	}
	stop = true
	<-done
	println(cnt > 0)
}
