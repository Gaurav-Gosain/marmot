package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

func main() {
	var conns atomic.Int32
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chunks" {
			f := w.(http.Flusher)
			for i := 0; i < 4; i++ {
				fmt.Fprintf(w, "chunk%d;", i)
				f.Flush()
			}
			return
		}
		fmt.Fprint(w, "ok ", r.URL.Path)
	}))
	ts.Config.ConnState = func(c net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	ts.Start()
	defer ts.Close()
	client := &http.Client{Transport: &http.Transport{}}
	for _, p := range []string{"/one", "/two", "/three"} {
		resp, _ := client.Get(ts.URL + p)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Println(string(b), resp.TransferEncoding, resp.ContentLength)
	}
	fmt.Println("connections:", conns.Load())
	resp, _ := client.Get(ts.URL + "/chunks")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println(string(b), resp.TransferEncoding, resp.ContentLength)
	// raw HTTP/1.1 on one connection with two pipelined requests
	c, _ := net.Dial("tcp", ts.Listener.Addr().String())
	fmt.Fprint(c, "GET /raw1 HTTP/1.1\r\nHost: x\r\n\r\nGET /raw2 HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
	br := bufio.NewReader(c)
	for i := 0; i < 2; i++ {
		r, err := http.ReadResponse(br, nil)
		if err != nil {
			fmt.Println(err)
			break
		}
		b, _ := io.ReadAll(r.Body)
		fmt.Println(r.Status, string(b), r.Close)
	}
	c.Close()
	client.CloseIdleConnections()
}
