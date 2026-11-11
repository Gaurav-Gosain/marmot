package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func main() {
	for _, s := range []string{`{}`, `[1,2,3]`, `{"a":}`, `[1,]`, `"unterminated`, `nul`, `{"a":1}extra`, ` 1 `, `-0.5e+10`, `01`, `"\ud800"`, `{"a":{"b":[null]}}`} {
		fmt.Println(json.Valid([]byte(s)), s)
	}
	var out bytes.Buffer
	json.Indent(&out, []byte(`{"a":[1,2,{"b":null}],"c":"d"}`), "", "\t")
	fmt.Println(out.String())
	out.Reset()
	json.Compact(&out, []byte("{ \"a\" : [ 1 , 2 ] }"))
	fmt.Println(out.String())
	out.Reset()
	json.HTMLEscape(&out, []byte(`"<a&b>"`))
	fmt.Println(out.String())
}
