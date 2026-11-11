package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

type S struct {
	A int  `json:"a"`
	B bool `json:"b"`
}

func main() {
	var s S
	err := json.Unmarshal([]byte(`{"a": "no"}`), &s)
	var ute *json.UnmarshalTypeError
	fmt.Println(errors.As(err, &ute), ute.Field, ute.Value, ute.Offset)
	err = json.Unmarshal([]byte(`{"a": 1`), &s)
	fmt.Println(err)
	err = json.Unmarshal([]byte(`{"a": 1} x`), &s)
	var se *json.SyntaxError
	fmt.Println(errors.As(err, &se), err)
	var nonPtr, nilTarget any = s, nil
	fmt.Println(json.Unmarshal([]byte(`{}`), nonPtr))
	fmt.Println(json.Unmarshal([]byte(`{}`), nilTarget))
	_, err = json.Marshal(math.NaN())
	fmt.Println(err)
	_, err = json.Marshal(make(chan int))
	fmt.Println(err)
	_, err = json.Marshal(map[bool]int{true: 1})
	fmt.Println(err)
	type cyc struct{ P *cyc }
	_, err = json.Marshal(struct{ F func() }{})
	fmt.Println(err)
}
