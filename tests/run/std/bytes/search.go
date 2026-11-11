package main

import "bytes"

func main() {
	s := []byte("go gopher go")
	println(bytes.Index(s, []byte("go")), bytes.LastIndex(s, []byte("go")), bytes.Index(s, []byte("")), bytes.Index(s, []byte("zz")))
	println(bytes.IndexByte(s, 'h'), bytes.IndexAny(s, "pq"), bytes.IndexRune(s, 'r'), bytes.IndexFunc(s, func(r rune) bool { return r == ' ' }))
	println(bytes.Contains(s, []byte("gopher")), bytes.ContainsAny(s, "xyz"), bytes.ContainsRune(s, 'o'))
	println(bytes.Count(s, []byte("go")), bytes.Count(s, []byte("")))
	println(bytes.HasPrefix(s, []byte("go ")), bytes.HasSuffix(s, []byte("go")), bytes.HasPrefix(s, nil))
	println(bytes.Equal(nil, []byte{}), bytes.Equal([]byte("a"), []byte("A")), bytes.EqualFold([]byte("a"), []byte("A")))
	println(bytes.Compare([]byte("a"), []byte("b")), bytes.Compare(nil, nil), bytes.Compare([]byte("ab"), []byte("a")))
}
