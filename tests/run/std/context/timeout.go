package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func slow(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	fmt.Println(slow(ctx, time.Millisecond))
	err := slow(ctx, 2*time.Second)
	fmt.Println(err, errors.Is(err, context.DeadlineExceeded))
	dl, ok := ctx.Deadline()
	fmt.Println(ok, !dl.IsZero())
	_, ok = context.Background().Deadline()
	fmt.Println(ok)
	d2, c2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer c2()
	fmt.Println(d2.Err())
	stop := context.AfterFunc(ctx, func() { fmt.Println("afterfunc ran") })
	time.Sleep(20 * time.Millisecond)
	fmt.Println(stop())
	fmt.Println(context.Background(), context.TODO())
	w := context.WithoutCancel(ctx)
	fmt.Println(w.Err())
}
