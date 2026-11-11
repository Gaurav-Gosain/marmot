package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/", HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "pref", Value: "dark", Path: "/app", MaxAge: 3600})
		fmt.Fprint(w, "logged in")
	})
	mux.HandleFunc("/app/me", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sid")
		p, _ := r.Cookie("pref")
		if err != nil {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "sid=", c.Value, " pref=", p.Value, " n=", len(r.Cookies()))
	})
	mux.HandleFunc("/other", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "cookies=", len(r.Cookies())) })
	mux.HandleFunc("/secret", func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "ann" || p != "pw" {
			w.Header().Set("WWW-Authenticate", `Basic realm="r"`)
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, "welcome ", u)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	get := func(p string) {
		resp, err := c.Get(ts.URL + p)
		if err != nil {
			fmt.Println(err)
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Println(p, resp.StatusCode, string(b))
	}
	get("/app/me")
	get("/login")
	get("/app/me")
	get("/other")
	u, _ := url.Parse(ts.URL)
	for _, ck := range jar.Cookies(u) {
		fmt.Println("jar:", ck.Name, ck.Value)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/secret", nil)
	resp, _ := http.DefaultClient.Do(req)
	fmt.Println(resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	req.SetBasicAuth("ann", "pw")
	resp, _ = http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	fmt.Println(resp.StatusCode, string(b))
	fmt.Println((&http.Cookie{Name: "a b", Value: "x;y", Path: "/p", Secure: true, SameSite: http.SameSiteStrictMode}).String())
}
