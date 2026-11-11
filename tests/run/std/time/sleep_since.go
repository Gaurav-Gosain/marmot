package main

import "time"

func main() {
	start := time.Now()
	time.Sleep(30 * time.Millisecond)
	e := time.Since(start)
	println(e >= 30*time.Millisecond, e < 2*time.Second)
	time.Sleep(0)
	time.Sleep(-5)
	deadline := time.Now().Add(40 * time.Millisecond)
	n := 0
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		n++
	}
	println(n >= 2, time.Until(deadline) <= 0)
	a, b := time.Now(), time.Now()
	println(!b.Before(a), b.Sub(a) >= 0)
	println(time.Now().Year() >= 2024, time.Now().Location() != nil)
}
