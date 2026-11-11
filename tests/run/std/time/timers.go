package main

import "time"

func main() {
	tm := time.NewTimer(10 * time.Millisecond)
	<-tm.C
	println("timer fired")
	select {
	case <-time.After(10 * time.Millisecond):
		println("after")
	case <-make(chan int):
	}
	tk := time.NewTicker(5 * time.Millisecond)
	n := 0
	for range tk.C {
		n++
		if n == 3 {
			tk.Stop()
			break
		}
	}
	println(n)
	t2 := time.NewTimer(time.Hour)
	println(t2.Stop())
	done := make(chan bool)
	time.AfterFunc(5*time.Millisecond, func() { done <- true })
	<-done
	println("afterfunc")
	select {
	case <-time.After(5 * time.Millisecond):
		println("timeout")
	case <-done:
	}
}
