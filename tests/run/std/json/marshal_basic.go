package main

import (
	"encoding/json"
	"fmt"
)

type Addr struct {
	Street string `json:"street"`
	Zip    string `json:"zip,omitempty"`
}

type Person struct {
	Name   string         `json:"name"`
	Age    int            `json:"age"`
	Email  *string        `json:"email"`
	Tags   []string       `json:"tags"`
	Addr   Addr           `json:"addr"`
	Meta   map[string]any `json:"meta,omitempty"`
	secret string
	Skip   string            `json:"-"`
	Score  float64           `json:"score"`
	Active bool              `json:"active,string"`
	Raw    json.RawMessage   `json:"raw"`
	Labels map[string]string `json:"labels"`
}

func main() {
	p := Person{Name: "Ann \"A\" <b>", Age: 30, Tags: []string{"x", "y"}, Addr: Addr{Street: "1 Main"}, Score: 1.5, Active: true, Raw: json.RawMessage(`{"k":[1,2]}`)}
	b, err := json.Marshal(p)
	fmt.Println(string(b), err)
	b, _ = json.MarshalIndent(map[string]any{"b": 1, "a": []int{1, 2}, "c": nil}, "", "  ")
	fmt.Println(string(b))
	for _, v := range []any{1, "s", 2.5, true, nil, []int(nil), []int{}, map[string]int(nil), 1e21, 1e-7, "é\u2028\x01", int64(1) << 60} {
		b, err := json.Marshal(v)
		fmt.Println(string(b), err)
	}
}
