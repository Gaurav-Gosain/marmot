package main

import "bytes"

func main() {
	b := bytes.NewBufferString("line1\nline2\nlast")
	for {
		l, err := b.ReadString('\n')
		println(len(l), err == nil)
		if err != nil {
			break
		}
	}
	bb := bytes.NewBuffer([]byte("abc"))
	bb.WriteString("def")
	line, err := bb.ReadBytes('d')
	println(string(line), err == nil, bb.String())
	var out bytes.Buffer
	out.Write(bb.Bytes())
	out.WriteTo(&bytes.Buffer{})
	println(out.Len())
	var e bytes.Buffer
	println(e.String(), e.Len())
	e.WriteString("x")
	e2 := bytes.NewBuffer(e.Bytes())
	println(e2.String())
}
