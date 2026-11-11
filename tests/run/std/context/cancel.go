package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type key string

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var mu sync.Mutex
	stopped := 0
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			mu.Lock()
			stopped++
			mu.Unlock()
		}()
	}
	fmt.Println(ctx.Err())
	cancel()
	wg.Wait()
	fmt.Println(stopped, ctx.Err(), errors.Is(ctx.Err(), context.Canceled), context.Cause(ctx))
	cancel()
	vctx := context.WithValue(context.Background(), key("user"), "ann")
	child, c2 := context.WithCancel(vctx)
	fmt.Println(child.Value(key("user")), child.Value(key("none")), child.Value("user"))
	c2()
	fmt.Println(child.Err(), vctx.Err())
	cctx, ccancel := context.WithCancelCause(context.Background())
	ccancel(errors.New("my reason"))
	fmt.Println(cctx.Err(), context.Cause(cctx))
	a, ca := context.WithCancel(context.Background())
	b, cb := context.WithCancel(a)
	defer cb()
	ca()
	<-b.Done()
	fmt.Println(b.Err())
	_ = time.Second
}
