package main

import "time"

func main() {
	t := time.NewTimer(time.Hour)
	println(t.Stop(), t.Stop())
	println(t.Reset(10 * time.Millisecond))
	<-t.C
	println("fired")
	println(t.Stop())
	t.Reset(5 * time.Millisecond)
	<-t.C
	println("again")
	af := time.AfterFunc(time.Hour, func() { println("never") })
	println(af.Stop())
	done := make(chan bool)
	time.AfterFunc(5*time.Millisecond, func() { done <- true })
	println(<-done)
}
