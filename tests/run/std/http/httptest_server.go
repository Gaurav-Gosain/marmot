package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

func main() {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "%s %s host=%t ua=%q", r.Method, r.URL.Path, r.Host != "", r.UserAgent())
	})
	ts := httptest.NewServer(h)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/a/b")
	b, _ := io.ReadAll(resp.Body)
	fmt.Println(resp.Status, string(b), err, strings.HasPrefix(ts.URL, "http://127.0.0.1:"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rec?x=1", strings.NewReader("body"))
	req.Header.Set("User-Agent", "unit")
	h.ServeHTTP(rec, req)
	res := rec.Result()
	b, _ = io.ReadAll(res.Body)
	fmt.Println(res.StatusCode, res.Header.Get("Content-Type"), string(b), rec.Code, rec.Body.Len())
	nf := httptest.NewRecorder()
	http.NotFound(nf, req)
	fmt.Println(nf.Code, strings.TrimSpace(nf.Body.String()))
	ts2 := httptest.NewUnstartedServer(h)
	ts2.Config.ErrorLog = nil
	ts2.Start()
	r2, _ := http.Get(ts2.URL)
	b2, _ := io.ReadAll(r2.Body)
	fmt.Println(string(b2) != "")
	ts2.Close()
	_, err = http.Get(ts2.URL)
	fmt.Println(err != nil)
}
