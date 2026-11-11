package main

import "bytes"

func main() {
	var nilb []byte
	empty := []byte{}
	println(bytes.Equal(nilb, empty), bytes.Compare(nilb, empty), nilb == nil, empty == nil)
	println(bytes.Index(nilb, nil), bytes.Index(empty, []byte("a")), bytes.Contains(nilb, nil))
	println(len(bytes.Split(nil, []byte(","))), len(bytes.Fields(nil)), string(bytes.Join(nil, nil)) == "")
	println(string(bytes.TrimSpace(nil)) == "", bytes.HasPrefix(nil, nil), bytes.Count(nil, nil))
	println(bytes.EqualFold([]byte("Straße"), []byte("STRASSE")), bytes.EqualFold([]byte("k"), []byte("K")))
	println(bytes.IndexByte([]byte("abc"), 'z'), bytes.LastIndexByte([]byte("abca"), 'a'))
}
