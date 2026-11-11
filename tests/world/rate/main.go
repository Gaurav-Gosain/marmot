package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"
)

func main() {
	l := rate.NewLimiter(rate.Limit(10), 3)
	now := time.Unix(1000, 0)
	for i := 0; i < 5; i++ {
		fmt.Println(i, l.AllowN(now, 1))
	}
	fmt.Println(l.AllowN(now.Add(100*time.Millisecond), 1), l.AllowN(now.Add(100*time.Millisecond), 1))
	fmt.Println(l.AllowN(now.Add(10*time.Second), 3), l.AllowN(now.Add(10*time.Second), 1))
	r := l.ReserveN(now.Add(20*time.Second), 3)
	fmt.Println(r.OK(), r.DelayFrom(now.Add(20*time.Second)))
	r2 := l.ReserveN(now.Add(20*time.Second), 2)
	fmt.Println(r2.OK(), r2.DelayFrom(now.Add(20*time.Second)))
	r2.CancelAt(now.Add(20 * time.Second))
	fmt.Println(l.Limit(), l.Burst(), rate.Every(250*time.Millisecond), rate.Inf)
	l.SetLimitAt(now.Add(30*time.Second), 2)
	fmt.Println(l.Limit(), l.TokensAt(now.Add(30*time.Second)) <= 3)
	fmt.Println(l.ReserveN(now.Add(31*time.Second), 10).OK())
	fast := rate.NewLimiter(rate.Inf, 0)
	fmt.Println(fast.Allow(), fast.AllowN(time.Now(), 1000))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w := rate.NewLimiter(1, 1)
	fmt.Println(w.Wait(ctx), w.Wait(ctx) != nil)
	st := time.Now()
	w2 := rate.NewLimiter(rate.Every(20*time.Millisecond), 1)
	for i := 0; i < 4; i++ {
		w2.Wait(context.Background())
	}
	fmt.Println(time.Since(st) >= 55*time.Millisecond)
}
