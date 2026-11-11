package main

import (
	"encoding/json"
	"fmt"
)

type Item struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Price float64  `json:"price"`
	Tags  []string `json:"tags"`
	Next  *Item    `json:"next"`
	Extra any      `json:"extra"`
	Count uint8    `json:"count"`
}

func main() {
	var it Item
	err := json.Unmarshal([]byte(`{"id": 7, "NAME": "case insens", "price": 2.5e1, "tags": ["a","b"], "next": {"id": 8}, "extra": {"x": [1, "y", null, true]}, "unknown": 1}`), &it)
	fmt.Println(err, it.ID, it.Name, it.Price, it.Tags, it.Next.ID, it.Extra)
	var bad Item
	fmt.Println(json.Unmarshal([]byte(`{"id": "str"}`), &bad))
	fmt.Println(json.Unmarshal([]byte(`{"count": 300}`), &bad))
	fmt.Println(json.Unmarshal([]byte(`{"id": 1,}`), &bad))
	fmt.Println(json.Unmarshal([]byte(``), &bad))
	fmt.Println(json.Unmarshal([]byte(`[1,2]`), &bad))
	var n Item
	fmt.Println(json.Unmarshal([]byte(`null`), &n), n.ID)
}
