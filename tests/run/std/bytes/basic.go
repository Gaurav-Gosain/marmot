package main

import "bytes"

func main() {
	a := []byte("hello world")
	println(bytes.Contains(a, []byte("wor")), bytes.Index(a, []byte("o")), bytes.LastIndex(a, []byte("o")))
	println(string(bytes.ToUpper(a)), bytes.Equal(a, []byte("hello world")), bytes.Compare([]byte("a"), []byte("b")))
	println(string(bytes.TrimSpace([]byte("  x  "))), string(bytes.Repeat([]byte("ab"), 2)))
	parts := bytes.Split([]byte("a,b,c"), []byte(","))
	println(len(parts), string(bytes.Join(parts, []byte("+"))))
	var buf bytes.Buffer
	buf.WriteString("abc")
	buf.WriteByte('d')
	buf.Write([]byte("ef"))
	println(buf.Len(), buf.String())
	x, _ := buf.ReadByte()
	println(x, buf.String())
	line := bytes.NewBufferString("l1\nl2\n")
	s, _ := line.ReadString('\n')
	println(s == "l1\n", line.Len())
	println(bytes.HasPrefix(a, []byte("he")), bytes.Count(a, []byte("l")))
}
