// jsonbench: JSON encode and decode of a slice of structs.
package main

import (
	"encoding/json"
	"fmt"
)

const n = 20000

type item struct {
	ID    int               `json:"id"`
	Name  string            `json:"name"`
	Price float64           `json:"price"`
	Tags  []string          `json:"tags"`
	Attrs map[string]string `json:"attrs"`
	Next  *item             `json:"next,omitempty"`
}

func main() {
	items := make([]item, n)
	for i := range items {
		items[i] = item{
			ID:    i,
			Name:  fmt.Sprint("item-", i),
			Price: float64(i%500) + 0.25,
			Tags:  []string{"a", "b", fmt.Sprint("t", i%10)},
			Attrs: map[string]string{"color": "red", "size": fmt.Sprint(i % 7)},
		}
		if i%5 == 0 {
			items[i].Next = &item{ID: -i, Name: "child"}
		}
	}
	total := 0
	var sum float64
	for r := 0; r < 20; r++ {
		data, err := json.Marshal(items)
		if err != nil {
			panic(err)
		}
		total += len(data)
		var back []item
		if err := json.Unmarshal(data, &back); err != nil {
			panic(err)
		}
		for i := range back {
			sum += back[i].Price + float64(len(back[i].Tags))
		}
	}
	fmt.Println(total, int64(sum*100))
}
