package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func main() {
	started := make(chan struct{})
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, "finished slow request")
	})
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := &http.Server{Handler: mux}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	res := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/slow")
		if err != nil {
			res <- err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		res <- string(b)
	}()
	<-started
	shut := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shut <- srv.Shutdown(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	_, err := http.Get("http://" + ln.Addr().String() + "/slow")
	fmt.Println("new request refused:", err != nil)
	close(release)
	fmt.Println(<-res)
	fmt.Println("shutdown:", <-shut)
	err = <-done
	fmt.Println(errors.Is(err, http.ErrServerClosed))
	srv2 := &http.Server{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	srv2.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) })
	go srv2.Serve(ln2)
	go http.Get("http://" + ln2.Addr().String())
	time.Sleep(50 * time.Millisecond)
	fmt.Println(errors.Is(srv2.Shutdown(ctx), context.DeadlineExceeded))
	srv2.Close()
}
