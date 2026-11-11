package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
)

func main() {
	dir, _ := os.MkdirTemp("", "marmot")
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>home</h1>"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nbeta\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"k":1}`), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "x.txt"), []byte("in sub"), 0o644)
	ts := httptest.NewServer(http.FileServer(http.FS(os.DirFS(dir))))
	defer ts.Close()
	for _, p := range []string{"/", "/a.txt", "/data.json", "/sub/x.txt", "/sub/", "/nope", "/../etc/passwd", "/a.txt/"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			fmt.Println(p, err)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%-16s %d %-28q %d\n", p, resp.StatusCode, resp.Header.Get("Content-Type"), len(b))
	}
	req, _ := http.NewRequest("GET", ts.URL+"/a.txt", nil)
	req.Header.Set("Range", "bytes=2-5")
	resp, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	fmt.Println(resp.StatusCode, resp.Header.Get("Content-Range"), string(b))
	req, _ = http.NewRequest("GET", ts.URL+"/a.txt", nil)
	req.Header.Set("If-Modified-Since", "Mon, 01 Jan 2100 00:00:00 GMT")
	resp, _ = http.DefaultClient.Do(req)
	fmt.Println(resp.StatusCode)
}
