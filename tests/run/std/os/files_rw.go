package main

import (
	"io"
	"os"
)

func main() {
	dir, err := os.MkdirTemp("", "marmot")
	if err != nil {
		println("mkdirtemp failed")
		return
	}
	defer os.RemoveAll(dir)
	p := dir + "/a.txt"
	println(os.WriteFile(p, []byte("hello\nworld\n"), 0o644) == nil)
	b, _ := os.ReadFile(p)
	println(len(b), string(b[:5]))
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("more")
	f.Close()
	f, _ = os.Open(p)
	buf := make([]byte, 5)
	n, _ := f.Read(buf)
	println(n, string(buf))
	off, _ := f.Seek(-4, io.SeekEnd)
	rest, _ := io.ReadAll(f)
	println(off, string(rest))
	n, err = f.ReadAt(buf, 6)
	println(n, string(buf))
	f.Close()
	_, err = f.Read(buf)
	println(err != nil)
	st, _ := os.Stat(p)
	println(st.Size(), st.Name(), st.IsDir(), st.Mode().IsRegular(), st.Mode().Perm())
}
