package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		fmt.Fprintf(w, "hello %s", r.URL.Query().Get("name"))
	})
	mux.HandleFunc("POST /echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(strings.ToUpper(string(b))))
	})
	mux.HandleFunc("/items/{id}", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "item ", r.PathValue("id"), " ", r.Method) })
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hello?name=redirected", http.StatusFound)
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", http.StatusTeapot) })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()
	base := "http://" + ln.Addr().String()
	show := func(resp *http.Response, err error) {
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%d %q ct=%q x=%q len=%d proto=%s\n", resp.StatusCode, b, resp.Header.Get("Content-Type"), resp.Header.Get("X-Test"), resp.ContentLength, resp.Proto)
	}
	show(http.Get(base + "/hello?name=marmot"))
	show(http.Post(base+"/echo", "text/plain", strings.NewReader("shout")))
	show(http.PostForm(base+"/echo", url.Values{"a": {"1"}, "b": {"x y"}}))
	show(http.Get(base + "/items/42"))
	show(http.Get(base + "/redir"))
	show(http.Get(base + "/fail"))
	show(http.Get(base + "/missing"))
	show(http.Head(base + "/hello"))
	req, _ := http.NewRequest("DELETE", base+"/items/7", nil)
	show(http.DefaultClient.Do(req))
	req, _ = http.NewRequest("PUT", base+"/hello", nil)
	show(http.DefaultClient.Do(req))
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(base + "/redir")
	fmt.Println(resp.StatusCode, resp.Header.Get("Location"), err)
}
