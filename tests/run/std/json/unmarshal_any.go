package main

import (
	"encoding/json"
	"fmt"
	"sort"
)

func main() {
	var v any
	json.Unmarshal([]byte(`{"n": 1, "f": 1.5, "s": "x", "b": true, "z": null, "a": [1, 2, {"k": "v"}], "o": {"p": {"q": 1e3}}}`), &v)
	m := v.(map[string]any)
	keys := make([]string, 0)
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s=%v (%T)\n", k, m[k], m[k])
	}
	var arr []any
	json.Unmarshal([]byte(`[1, "two", [3], null]`), &arr)
	fmt.Println(arr, len(arr))
	var big map[string]int64
	json.Unmarshal([]byte(`{"a": 9007199254740993}`), &big)
	fmt.Println(big["a"])
	var f any
	json.Unmarshal([]byte(`9007199254740993`), &f)
	fmt.Println(f)
}
