package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, err := url.Parse("https://user:pa%20ss@example.com:8080/a/b%2Fc/d?x=1&y=two+words&x=3&z=%E4%B8%96#frag")
	fmt.Println(err, u.Scheme, u.Host, u.Hostname(), u.Port(), u.Path, u.RawPath, u.Fragment, u.User.Username())
	pw, _ := u.User.Password()
	fmt.Println(pw, u.RawQuery, u.String())
	q := u.Query()
	fmt.Println(q["x"], q.Get("y"), q.Get("z"), q.Get("none"), q.Has("x"), q.Encode())
	v := url.Values{}
	v.Set("b", "2 3")
	v.Add("a", "1&2")
	v.Add("a", "é")
	fmt.Println(v.Encode())
	fmt.Println(url.QueryEscape("a b&c=d/é"), url.PathEscape("a b/c"))
	s, err := url.QueryUnescape("a%20b+c%zz")
	fmt.Println(s, err)
	_, err = url.Parse("http://[::1")
	fmt.Println(err)
	r, _ := url.Parse("../x/y?q=1")
	base, _ := url.Parse("http://h/a/b/c")
	fmt.Println(base.ResolveReference(r), r.IsAbs(), base.IsAbs())
	m, _ := url.ParseQuery("a=1&a=2&b=%41")
	fmt.Println(m)
}
