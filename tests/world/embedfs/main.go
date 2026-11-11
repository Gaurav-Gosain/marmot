package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
)

//go:embed hello.txt
var hello string

//go:embed hello.txt
var helloBytes []byte

//go:embed assets
var assets embed.FS

//go:embed assets/*.txt assets/data.json
var some embed.FS

//go:embed all:assets
var all embed.FS

func main() {
	fmt.Printf("%q %d\n", hello, len(helloBytes))
	fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		fmt.Println("assets:", p, d.IsDir())
		return nil
	})
	fs.WalkDir(all, ".", func(p string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			fmt.Println("all:", p)
		}
		return nil
	})
	b, err := fs.ReadFile(some, "assets/a.txt")
	fmt.Println(string(b), err)
	_, err = fs.ReadFile(some, "assets/sub/b.txt")
	fmt.Println(err != nil)
	sub, _ := fs.Sub(assets, "assets")
	m, _ := fs.Glob(sub, "*.txt")
	fmt.Println(m)
	f, _ := assets.Open("assets/sub/b.txt")
	st, _ := f.Stat()
	fmt.Println(st.Name(), st.Size(), st.Mode().IsRegular())
	ents, _ := assets.ReadDir("assets")
	for _, e := range ents {
		fmt.Print(e.Name(), " ")
	}
	fmt.Println()
	ts := httptest.NewServer(http.FileServer(http.FS(sub)))
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/data.json")
	body, _ := io.ReadAll(resp.Body)
	fmt.Println(resp.StatusCode, resp.Header.Get("Content-Type"), strings.TrimSpace(string(body)))
}
