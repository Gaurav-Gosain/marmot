package main

import (
	"encoding/json"
	"fmt"
)

type Base struct {
	ID   int    `json:"id"`
	Kind string `json:"kind"`
}

type Inner struct{ X, Y int }

type Doc struct {
	Base
	*Inner
	Kind  string  `json:"kind"`
	Opt   *int    `json:"opt,omitempty"`
	Zero  int     `json:"zero,omitempty"`
	Slice []int   `json:"slice,omitempty"`
	Iface any     `json:"iface,omitempty"`
	Fl    float32 `json:"fl"`
	Arr   [2]bool `json:"arr"`
	Bytes []byte  `json:"bytes"`
	Rune  rune    `json:"rune"`
	Ptr   **int   `json:"ptr"`
}

func main() {
	d := Doc{Base: Base{1, "base"}, Kind: "outer", Inner: &Inner{3, 4}, Fl: 0.1, Bytes: []byte("hi!")}
	b, err := json.Marshal(d)
	fmt.Println(string(b), err)
	var back Doc
	fmt.Println(json.Unmarshal(b, &back), back.ID, back.Kind, back.X, string(back.Bytes))
}
