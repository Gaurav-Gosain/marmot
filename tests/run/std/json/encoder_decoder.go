package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Ev struct {
	Type string `json:"type"`
	N    int    `json:"n"`
}

func main() {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.Encode(Ev{"a", 1})
	enc.SetIndent(">", " ")
	enc.Encode(map[string]int{"k": 2})
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "")
	enc.Encode("<tag>&")
	fmt.Print(buf.String())
	dec := json.NewDecoder(strings.NewReader(`{"type":"x","n":1} {"type":"y","n":2}
	[1,2] "tail"`))
	for {
		var v any
		if err := dec.Decode(&v); err == io.EOF {
			break
		} else if err != nil {
			fmt.Println("err", err)
			break
		}
		fmt.Println(v)
	}
	d2 := json.NewDecoder(strings.NewReader(`{"type":"x","bogus":1}`))
	d2.DisallowUnknownFields()
	var e Ev
	fmt.Println(d2.Decode(&e))
	d3 := json.NewDecoder(strings.NewReader(`{"n": 12345678901234567890}`))
	d3.UseNumber()
	var m map[string]any
	d3.Decode(&m)
	fmt.Printf("%v %T\n", m["n"], m["n"])
}
