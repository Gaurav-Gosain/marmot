package main

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"strings"
)

type Header struct {
	Magic uint32
	Ver   uint16
	Flags uint16
	Size  int64
}

func main() {
	var buf bytes.Buffer
	h := Header{0xcafebabe, 2, 0x8001, -5}
	binary.Write(&buf, binary.BigEndian, h)
	fmt.Printf("%x %d\n", buf.Bytes(), buf.Len())
	var back Header
	binary.Read(bytes.NewReader(buf.Bytes()), binary.BigEndian, &back)
	fmt.Println(back == h, back)
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, 0x01020304)
	binary.BigEndian.PutUint16(b[4:], 0xabcd)
	fmt.Println(b, binary.LittleEndian.Uint64(b), binary.BigEndian.Uint32(b))
	vb := binary.AppendUvarint(nil, 300)
	vb = binary.AppendVarint(vb, -300)
	u, n := binary.Uvarint(vb)
	i, m := binary.Varint(vb[n:])
	fmt.Println(vb, u, n, i, m)
	r := csv.NewReader(strings.NewReader("a,b,c\n1,\"two, 2\",3\n\"multi\nline\",x,\"q\"\"uote\"\n"))
	recs, err := r.ReadAll()
	fmt.Printf("%q %v\n", recs, err)
	var out strings.Builder
	w := csv.NewWriter(&out)
	w.Write([]string{"x", "y,z", "q\"r", "line\nbreak", ""})
	w.Flush()
	fmt.Printf("%q\n", out.String())
	_, err = csv.NewReader(strings.NewReader("a,b\n1\n")).ReadAll()
	fmt.Println(err)
}
