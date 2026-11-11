package main

import (
	"encoding/json"
	"fmt"
	"math"
)

type Inner struct {
	A int     `json:"a"`
	B string  `json:"b,omitempty"`
	C *int    `json:"c"`
	D []int   `json:"d"`
	E float64 `json:"e"`
}

type Outer struct {
	Inner
	Name   string              `json:"name"`
	M      map[string]int      `json:"m"`
	Any    any                 `json:"any"`
	Skip   string              `json:"-"`
	Str    int                 `json:"str,string"`
	Raw    json.RawMessage     `json:"raw"`
	Nested []Inner             `json:"nested"`
	Ptr    *Inner              `json:"ptr,omitempty"`
	B      []byte              `json:"bytes"`
	F32    float32             `json:"f32"`
	Tags   map[string][]string `json:"tags"`
	priv   int
}

func main() {
	seven := 7
	o := Outer{Inner: Inner{1, "", &seven, nil, 2.5}, Name: "n\"<>& é", M: map[string]int{"z": 1, "a": 2}, Any: []any{1, "x", nil, 2.5, map[string]any{"k": true}},
		Skip: "s", Str: 12, Raw: json.RawMessage(`{"r":[1,2]}`), Nested: []Inner{{A: 2}}, B: []byte("bytes!"), F32: 0.1, Tags: map[string][]string{"t": {"x"}}}
	b, err := json.Marshal(o)
	fmt.Println(string(b), err)
	b, _ = json.MarshalIndent(map[string]any{"b": []int{}, "a": map[string]int{}, "c": []int{1}}, ">", "  ")
	fmt.Println(string(b))
	for _, v := range []any{0, -1, 1.0, 1e21, 1e-7, 0.1, float32(0.1), math.MaxInt64, uint64(math.MaxUint64), "\x00\x1f\"\\/\t", true, nil, int8(-3), 100000000000000000000.0, 1.5e300, 5e-324} {
		b, err := json.Marshal(v)
		fmt.Println(string(b), err)
	}
	var back Outer
	err = json.Unmarshal(b, &back)
	fmt.Println(err)
	in := `{"a": 5, "b": "bb", "c": 9, "d": [1,2,3], "e": 1e2, "name": "xé😀", "m": {"k": 3}, "any": {"n": [1, {"z": null}]}, "str": "34", "raw": [1, 2 ], "nested": [{"a": 1}, {"a": 2, "d": []}], "bytes": "aGk=", "f32": 3.14159274, "unknown": 1, "tags": {"q": ["1", "2"]}}`
	var o2 Outer
	err = json.Unmarshal([]byte(in), &o2)
	fmt.Println(err, o2.A, o2.B, *o2.C, o2.D, o2.E, o2.Name, o2.M, o2.Any, o2.Str, string(o2.Raw), len(o2.Nested), string(o2.Bytes()), o2.F32, o2.Tags)
	for _, bad := range []string{`{"a": "x"}`, `{"a": 1.5}`, `{"d": {}}`, `[`, `{"a": 1,}`, `{"str": 5}`, ``, `{"a": 99999999999999999999}`, `nul`, `{"bytes": "!!"}`} {
		var x Outer
		fmt.Printf("%q: %v\n", bad, json.Unmarshal([]byte(bad), &x))
	}
}

func (o Outer) Bytes() []byte { return o.B }
