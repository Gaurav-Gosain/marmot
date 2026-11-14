package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

func main() {
	gotCancel := make(chan bool, 1)
	entered := make(chan struct{}, 3)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-r.Context().Done():
			gotCancel <- true
		case <-time.After(5 * time.Second):
			gotCancel <- false
		}
	}))
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
	errc := make(chan error, 1)
	go func() {
		_, err := http.DefaultClient.Do(req)
		errc <- err
	}()
	<-entered // cancel once the server has the request
	cancel()
	err := <-errc
	fmt.Println(errors.Is(err, context.Canceled))
	fmt.Println("server saw cancel:", <-gotCancel)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, "GET", ts.URL, nil)
	_, err = http.DefaultClient.Do(req2)
	fmt.Println(errors.Is(err, context.DeadlineExceeded))
	handlerDone(gotCancel)
	c := &http.Client{Timeout: 40 * time.Millisecond}
	_, err = c.Get(ts.URL)
	var to interface{ Timeout() bool }
	fmt.Println(errors.As(err, &to) && to.Timeout())
	handlerDone(gotCancel)
}

// handlerDone waits for the handler of a request that timed out on the
// client side. The deadline can pass before the request reaches the server
// (a slow run), and then no handler runs: give up after the handler's own
// five seconds.
func handlerDone(gotCancel chan bool) {
	select {
	case <-gotCancel:
	case <-time.After(6 * time.Second):
	}
}
