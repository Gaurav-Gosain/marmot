package main

type Reader interface{ Read() string }
type Writer interface{ Write(s string) }
type RW interface {
	Reader
	Writer
}

type File struct{ data string }

func (f *File) Read() string   { return f.data }
func (f *File) Write(s string) { f.data += s }

type Logger struct {
	Writer
	prefix string
}

func (l Logger) Log(s string) { l.Write(l.prefix + s) }

func main() {
	f := &File{}
	var rw RW = f
	rw.Write("a")
	rw.Write("b")
	println(rw.Read())
	var r Reader = rw
	println(r.Read())
	w, ok := r.(Writer)
	println(ok)
	w.Write("c")
	println(f.data)
	l := Logger{f, ">"}
	l.Log("x")
	println(f.data)
	_, ok = r.(interface{ Close() })
	println(ok)
}
